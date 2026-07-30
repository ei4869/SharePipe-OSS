package httpapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParseRange(t *testing.T) {
	var contract struct {
		Ranges []struct {
			Header     string `json:"header"`
			Size       int64  `json:"size"`
			Valid      bool   `json:"valid"`
			Start, End int64
		} `json:"ranges"`
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "tests", "contracts", "reliability.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &contract); err != nil {
		t.Fatal(err)
	}
	for _, test := range contract.Ranges {
		result, err := parseRange(test.Header, test.Size)
		if !test.Valid {
			if err == nil {
				t.Fatalf("expected %s to fail", test.Header)
			}
			continue
		}
		if err != nil || result.Start != test.Start || result.End != test.End {
			t.Fatalf("%s: %#v %v", test.Header, result, err)
		}
	}
}
