package netactuate

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/netactuate/gona/gona"
)

// Create follows the build job, which answers while GET server still returns 422.
func TestCreateWaitsOnBuildJob(t *testing.T) {
	var jobCalls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "cloud/jobs/build/"):
			st := 3
			if atomic.AddInt32(&jobCalls, 1) > 2 {
				st = 5
			}
			fmt.Fprintf(w, `{"code":200,"data":{"id":9,"command":"build","status":%d}}`, st)
		case atomic.LoadInt32(&jobCalls) <= 2:
			w.WriteHeader(http.StatusUnprocessableEntity)
			w.Write([]byte(`{"code":422,"fields":{"mbpkgid":["The mbpkgid must be a valid mbpkgid"]}}`))
		default:
			w.Write([]byte(`{"code":200,"data":{"mbpkgid":1,"status":"RUNNING"}}`))
		}
	}))
	defer srv.Close()

	c := gona.NewClientCustom("k", srv.URL+"/")
	if d := wait4JobStatus("build", 9, c); d != nil {
		t.Fatalf("build job wait failed: %v", d)
	}
	if _, d := wait4Status(1, "RUNNING", c); d.HasError() {
		t.Fatalf("status after build job failed: %v", d)
	}
}

func TestWait4JobStatusFailsAboveFive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"code":200,"data":{"id":9,"command":"build","status":6}}`))
	}))
	defer srv.Close()

	c := gona.NewClientCustom("k", srv.URL+"/")
	if d := wait4JobStatus("build", 9, c); !d.HasError() {
		t.Fatal("a failed job must fail the wait")
	}
}

func TestCreateWaitErrorExplainsTaint(t *testing.T) {
	got := createWaitError(diag.Errorf("timeout waiting for job build #9 to complete"))
	if len(got) != 1 || !got.HasError() || !strings.Contains(got[0].Summary, "timeout waiting for job build #9") {
		t.Fatalf("the original error must be kept: %v", got)
	}
	if !strings.Contains(got[0].Detail, "created but is marked tainted") || !strings.Contains(got[0].Detail, "untaint and refresh") {
		t.Fatalf("missing the taint note: %q", got[0].Detail)
	}
}

func TestCreateWaitErrorKeepsExistingDetail(t *testing.T) {
	got := createWaitError(diag.Diagnostics{{Severity: diag.Error, Summary: "boom", Detail: "original detail"}})
	if !strings.HasPrefix(got[0].Detail, "original detail") || !strings.Contains(got[0].Detail, "untaint and refresh") {
		t.Fatalf("existing detail must be kept and the note appended: %q", got[0].Detail)
	}
}
