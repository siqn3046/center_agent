package httpapi

import (
	"net/url"
	"strings"
	"testing"
)

func TestValidateInstallNodeQueryWhitelist(t *testing.T) {
	q := url.Values{}
	q.Set("token", "abc")
	q.Set("evil", "1")
	_, err := validateInstallNodeQuery(q)
	if err == nil || err.Error() != "unsupported query parameter: evil" {
		t.Fatalf("got %v", err)
	}
}

func TestValidateInstallNodeQueryMissingToken(t *testing.T) {
	q := url.Values{}
	q.Set("os", "linux")
	_, err := validateInstallNodeQuery(q)
	if err == nil || err.Error() != "query token required" {
		t.Fatalf("got %v", err)
	}
}

func TestValidateInstallNodeQueryWindows(t *testing.T) {
	q := url.Values{}
	q.Set("token", "abc")
	q.Set("os", "windows")
	_, err := validateInstallNodeQuery(q)
	if err == nil || err.Error() != "暂未支持" {
		t.Fatalf("got %v", err)
	}
}

func TestValidateInstallNodeQueryInstallDirBad(t *testing.T) {
	q := url.Values{}
	q.Set("token", "abc")
	q.Set("install_dir", "/opt;;")
	_, err := validateInstallNodeQuery(q)
	if err == nil {
		t.Fatal("expected err")
	}
}

func TestValidateInstallNodeQueryInterval(t *testing.T) {
	q := url.Values{}
	q.Set("token", "abc")
	q.Set("interval", "3")
	_, err := validateInstallNodeQuery(q)
	if err == nil {
		t.Fatal("expected err")
	}
	q.Del("interval")
	q.Set("interval", "10")
	_, err = validateInstallNodeQuery(q)
	if err != nil {
		t.Fatal(err)
	}
}

func TestBuildInstallNodeShellNoEval(t *testing.T) {
	sh := buildInstallNodeShell("https://x.example", 1, "tok", "https://x/a", "aa", "y: k\n", installNodeOpts{
		ServiceName: "XrayR",
		InstallDir:  "/opt/xrayr",
		Interval:    10,
		ExcludeNICs: "lo",
		MountPoints: "/",
	})
	if strings.Contains(sh, "eval") {
		t.Fatal("script must not contain eval")
	}
	if strings.Contains(sh, "bash -c") {
		t.Fatal("script must not contain bash -c")
	}
}
