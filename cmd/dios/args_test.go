package main

import (
	"reflect"
	"testing"

	"github.com/ssajaia/dios/internal/testutil"
)

func TestParseSyncArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    syncArgs
		wantErr string
	}{
		{
			name: "paths only",
			args: []string{"a", "b"},
			want: syncArgs{source: "a", destination: "b"},
		},
		{
			name: "long flags in any position",
			args: []string{"--not-sure", "a", "--prevent-delete", "b"},
			want: syncArgs{source: "a", destination: "b", notSure: true, preventDelete: true},
		},
		{
			name: "short flag names",
			args: []string{"a", "b", "--ns", "--pd"},
			want: syncArgs{source: "a", destination: "b", notSure: true, preventDelete: true},
		},
		{
			name: "skip is repeatable",
			args: []string{"-s", "main.py", "a", "--skip", "readme.md", "b"},
			want: syncArgs{source: "a", destination: "b", skip: []string{"main.py", "readme.md"}},
		},
		{
			name: "skip with equals sign",
			args: []string{"a", "b", "--skip=main.py"},
			want: syncArgs{source: "a", destination: "b", skip: []string{"main.py"}},
		},
		{
			name: "skip pattern is normalized",
			args: []string{"a", "b", "-s", "./docs/", "-s", "*.log"},
			want: syncArgs{source: "a", destination: "b", skip: []string{"docs", "*.log"}},
		},
		{name: "skip without value", args: []string{"a", "b", "-s"}, wantErr: "-s requires a value"},
		{name: "skip followed by flag", args: []string{"a", "b", "--skip", "--pd"}, wantErr: "--skip requires a value"},
		{name: "empty skip pattern", args: []string{"a", "b", "--skip="}, wantErr: "skip pattern is empty"},
		{name: "malformed skip pattern", args: []string{"a", "b", "-s", "[abc"}, wantErr: "invalid skip pattern"},
		{name: "unknown option", args: []string{"--force", "a", "b"}, wantErr: "unknown option: --force"},
		{name: "dry-run is gone", args: []string{"--dry-run", "a", "b"}, wantErr: "unknown option: --dry-run"},
		{name: "no arguments", args: nil, wantErr: "expected <source> and <destination>"},
		{name: "one path", args: []string{"a"}, wantErr: "expected <source> and <destination>"},
		{name: "three paths", args: []string{"a", "b", "c"}, wantErr: "expected <source> and <destination>"},
		{name: "skip takes exactly one value", args: []string{"-s", "x.txt", "y.txt", "a", "b"}, wantErr: "got 3 path(s)"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSyncArgs(tt.args)
			testutil.CheckErr(t, err, tt.wantErr)
			if tt.wantErr != "" {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parsed = %+v, want %+v", got, tt.want)
			}
		})
	}
}
