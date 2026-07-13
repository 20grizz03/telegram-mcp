package config

import "testing"

func TestParseEnableWrite(t *testing.T) {
	cases := map[string]bool{
		"1": true, "true": true, "TRUE": true, "yes": true, "Yes": true,
		"": false, "0": false, "false": false, "no": false, "nope": false,
	}
	for in, want := range cases {
		if got := parseEnableWrite(in); got != want {
			t.Errorf("parseEnableWrite(%q) = %v, want %v", in, got, want)
		}
	}
}
