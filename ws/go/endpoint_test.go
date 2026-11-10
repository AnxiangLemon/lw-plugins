package lwplugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEndpointExecutableDirectoryAndWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	program := filepath.Join(root, "portable", "group-manager.exe")
	working := filepath.Join(root, "another", "work")
	paths := endpointCandidates(program, working)
	if paths[0] != filepath.Join(root, "portable", "plugin-endpoint.json") || paths[2] != filepath.Join(working, "plugin-endpoint.json") {
		t.Fatalf("查找顺序错误: %v", paths)
	}
	t.Setenv("LW_PLUGIN_URL", "")
	t.Setenv("LW_PLUGIN_TOKEN", "")
	t.Setenv("LW_PLUGIN_ENDPOINT_FILE", "")
	if err := os.MkdirAll(working, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(working, "plugin-endpoint.json"), []byte(`{"host":"127.0.0.1","port":19999,"token":"test-only"}`), 0600); err != nil {
		t.Fatal(err)
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir(working); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)
	e, err := ResolveEndpoint("", "", "")
	if err != nil || e.Token != "test-only" || e.URL != "ws://127.0.0.1:19999/" {
		t.Fatalf("运行目录的端点未发现: %+v %v", e, err)
	}
}
