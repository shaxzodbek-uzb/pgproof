package destination

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/shaxzodbek-uzb/pgproof/internal/config"
)

func TestLocalRoundTrip(t *testing.T) {
	dir := t.TempDir()
	d, err := New(config.Destination{Type: config.TypeLocal, Name: "disk", Path: dir})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	ctx := context.Background()

	content := "hello backup"
	if err := d.Put(ctx, "app/app-1.dump", strings.NewReader(content), int64(len(content))); err != nil {
		t.Fatalf("put: %v", err)
	}

	arts, err := d.List(ctx, "app/")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(arts) != 1 || arts[0].Key != "app/app-1.dump" {
		t.Fatalf("list = %+v", arts)
	}

	rc, err := d.Get(ctx, "app/app-1.dump")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	got, _ := io.ReadAll(rc)
	rc.Close()
	if !bytes.Equal(got, []byte(content)) {
		t.Fatalf("get content = %q", got)
	}

	if err := d.Delete(ctx, "app/app-1.dump"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := d.Delete(ctx, "app/app-1.dump"); err != nil {
		t.Fatalf("delete missing should be nil: %v", err)
	}
}

func TestLocalTester(t *testing.T) {
	dir := t.TempDir()
	d, _ := New(config.Destination{Type: config.TypeLocal, Name: "disk", Path: dir + "/nested"})
	tester, ok := d.(Tester)
	if !ok {
		t.Fatal("local should implement Tester")
	}
	if err := tester.Test(context.Background()); err != nil {
		t.Fatalf("test: %v", err)
	}
}

func TestParseS3Endpoint(t *testing.T) {
	cases := []struct {
		raw, region, host string
		secure            bool
	}{
		{"", "us-east-1", "s3.amazonaws.com", true},
		{"", "eu-west-1", "s3.eu-west-1.amazonaws.com", true},
		{"https://x.r2.cloudflarestorage.com", "auto", "x.r2.cloudflarestorage.com", true},
		{"http://localhost:9000", "", "localhost:9000", false},
		{"minio.local/", "", "minio.local", true},
	}
	for _, c := range cases {
		host, secure := parseS3Endpoint(c.raw, c.region)
		if host != c.host || secure != c.secure {
			t.Errorf("parseS3Endpoint(%q,%q) = %q,%v; want %q,%v", c.raw, c.region, host, secure, c.host, c.secure)
		}
	}
}
