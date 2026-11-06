package netactuate

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/netactuate/gona/gona"
)

// readWith422 runs resourceServerRead for server 7 whose GET answers the platform's 422
// "mbpkgid must be valid", with the build job endpoint answering jobCode/jobBody.
func readWith422(t *testing.T, buildJobID int, jobCode int, jobBody string) (id string, diags diag.Diagnostics) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "cloud/jobs/build/") {
			w.WriteHeader(jobCode)
			w.Write([]byte(jobBody))
			return
		}
		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Write([]byte(`{"code":422,"fields":{"mbpkgid":["The mbpkgid must be a valid mbpkgid"]}}`))
	}))
	t.Cleanup(srv.Close)

	d := resourceServer().TestResourceData()
	d.SetId("7")
	if buildJobID != 0 {
		if err := d.Set("build_job_id", buildJobID); err != nil {
			t.Fatal(err)
		}
	}
	diags = resourceServerRead(context.Background(), d, &ProviderClients{V2: gona.NewClientCustom("k", srv.URL+"/")})
	return d.Id(), diags
}

func jobBody(status int) string {
	return fmt.Sprintf(`{"code":200,"data":{"id":9,"command":"build","status":%d}}`, status)
}

func TestReadKeepsServerWhileBuildJobRuns(t *testing.T) {
	for _, st := range []int{1, 2, 3, 4} {
		id, diags := readWith422(t, 9, 200, jobBody(st))
		if id != "7" {
			t.Errorf("job status %d: server was dropped from state", st)
		}
		if len(diags) != 0 {
			t.Errorf("job status %d: want no messages, got %v", st, diags)
		}
	}
}

func TestReadDropsServerWhenBuildDoneAndServerGone(t *testing.T) {
	id, diags := readWith422(t, 9, 200, jobBody(5))
	if id != "" || diags.HasError() {
		t.Fatalf("finished build with the server gone: want dropped without error, got id=%q diags=%v", id, diags)
	}
}

func TestReadDropsServerWithoutBuildJobID(t *testing.T) {
	id, diags := readWith422(t, 0, 200, jobBody(3))
	if id != "" || diags.HasError() {
		t.Fatalf("no build job id must keep today's behavior (drop): id=%q diags=%v", id, diags)
	}
}

func TestReadDropsServerWhenBuildFailed(t *testing.T) {
	id, diags := readWith422(t, 9, 200, jobBody(6))
	if id != "" || len(diags) != 0 {
		t.Fatalf("failed build with the server gone: want dropped with no messages, got id=%q diags=%v", id, diags)
	}
}

func TestReadDropsServerSilentlyWhenBuildJobRecordMissing(t *testing.T) {
	id, diags := readWith422(t, 9, 404, `{"code":404,"message":"Not Found"}`)
	if id != "" || diags.HasError() || len(diags) != 0 {
		t.Fatalf("missing job record with the server gone: want dropped without a message, got id=%q diags=%v", id, diags)
	}
}

func TestReadKeepsServerWhenJobLookupFails(t *testing.T) {
	id, diags := readWith422(t, 9, 500, `{"code":500,"message":"Internal Server Error"}`)
	if id != "7" || !diags.HasError() {
		t.Fatalf("a failed job lookup must keep the server and error: id=%q diags=%v", id, diags)
	}
}

// readInstalled0 runs resourceServerRead for a server the API can read but that is not installed
// yet (installed 0), with state holding hostname "web-1" and, optionally, a build job.
func readInstalled0(t *testing.T, buildJobID int, jobCode int, jobBody string) (hostname string, diags diag.Diagnostics) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "cloud/jobs/build/") {
			w.WriteHeader(jobCode)
			w.Write([]byte(jobBody))
			return
		}
		w.Write([]byte(`{"code":200,"data":{"mbpkgid":7,"installed":0,"status":"PENDING","name":"","package":"VR1x1x25"}}`))
	}))
	t.Cleanup(srv.Close)

	d := resourceServer().TestResourceData()
	d.SetId("7")
	d.Set("hostname", "web-1")
	if buildJobID != 0 {
		d.Set("build_job_id", buildJobID)
	}
	diags = resourceServerRead(context.Background(), d, &ProviderClients{V2: gona.NewClientCustom("k", srv.URL+"/")})
	return d.Get("hostname").(string), diags
}

func TestReadKeepsHostnameWhileServerIsBuilding(t *testing.T) {
	host, diags := readInstalled0(t, 9, 200, jobBody(3))
	if host != "web-1" {
		t.Fatalf("hostname was blanked while the build runs: %q", host)
	}
	if len(diags) != 0 {
		t.Fatalf("want no messages, got %v", diags)
	}
}

func TestReadBlanksHostnameWithoutRunningBuildJob(t *testing.T) {
	if host, _ := readInstalled0(t, 0, 200, jobBody(3)); host != "" {
		t.Errorf("no build job id: keep today's behaviour, got hostname %q", host)
	}
	if host, _ := readInstalled0(t, 9, 200, jobBody(5)); host != "" {
		t.Errorf("finished build job: keep today's behaviour, got hostname %q", host)
	}
}
