package cli

import (
	"reflect"
	"testing"
)

func TestRewriteArgs(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"empty", nil, []string{"mask"}},
		{"subcommand", []string{"mask", "-"}, []string{"mask", "-"}},
		{"corpus subcommand", []string{"corpus", "list"}, []string{"corpus", "list"}},
		{"global then subcommand", []string{"--mapping", "m.sqlite", "mask", "-"}, []string{"--mapping", "m.sqlite", "mask", "-"}},
		{"global then file", []string{"--verbose", "log.txt"}, []string{"mask", "--verbose", "log.txt"}},
		{"dash file", []string{"-"}, []string{"mask", "-"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RewriteArgs(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("RewriteArgs()=%v want %v", got, tt.want)
			}
		})
	}
}
