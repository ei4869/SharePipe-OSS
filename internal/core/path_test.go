package core

import (
	"reflect"
	"testing"
)

func TestNormalizeRelativePath(t *testing.T) {
	path, err := NormalizeRelativePath("项目\\源文件/main.go")
	if err != nil || path != "项目/源文件/main.go" {
		t.Fatalf("unexpected normalized path %q: %v", path, err)
	}
	for _, invalid := range []string{"../escape", "/absolute", "C:/drive", "a//b", "a/./b", "a/../b", ""} {
		if _, err := NormalizeRelativePath(invalid); err == nil {
			t.Fatalf("expected %q to be rejected", invalid)
		}
	}
}

func TestParentDirectories(t *testing.T) {
	actual := ParentDirectories("a/b/c.txt")
	expected := []string{"a", "a/b"}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("unexpected parents: %#v", actual)
	}
}
