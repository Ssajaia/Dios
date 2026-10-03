package alias

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ssajaia/dios/internal/testutil"
)

func TestInitAliases(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, root string)
		want    string
		wantErr string
	}{
		{
			name:  "creates directory and file",
			setup: func(t *testing.T, root string) {},
			want:  Template,
		},
		{
			name: "directory already exists",
			setup: func(t *testing.T, root string) {
				testutil.Mkdir(t, root, ".config")
			},
			want: Template,
		},
		{
			name: "existing file is not overwritten",
			setup: func(t *testing.T, root string) {
				testutil.WriteFile(t, filepath.Join(root, ".config", "aliases"), "mine = \"/x\"\n")
			},
			want:    "mine = \"/x\"\n",
			wantErr: "already initialized",
		},
		{
			name: "existing empty file is not overwritten",
			setup: func(t *testing.T, root string) {
				testutil.WriteFile(t, filepath.Join(root, ".config", "aliases"), "")
			},
			want:    "",
			wantErr: "already initialized",
		},
		{
			name: "config path is a file",
			setup: func(t *testing.T, root string) {
				testutil.WriteFile(t, filepath.Join(root, ".config"), "not a directory")
			},
			wantErr: "cannot create directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			tt.setup(t, root)
			file := filepath.Join(root, ".config", "aliases")

			testutil.CheckErr(t, Init(file), tt.wantErr)

			if tt.wantErr == "cannot create directory" {
				return
			}
			got, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("file content = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestInitTemplateIsValidAliasFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), ".config", "aliases")
	if err := Init(file); err != nil {
		t.Fatal(err)
	}

	aliases, err := Load(file)
	if err != nil {
		t.Fatalf("template does not load: %v", err)
	}
	if len(aliases) != 0 {
		t.Errorf("template defines aliases: %v", aliases)
	}
}
