package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// 허용 루트 검사가 실제로 막아야 할 것을 막는지 본다.
// 여기가 뚫리면 시작 위치 제한이 아무 의미가 없다.
func TestResolveDir(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "proj")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}

	// 루트 옆에 형제 디렉터리를 둔다. 이름이 루트로 시작하게 만들어
	// 문자열 앞자리 비교로 짠 구현이라면 통과해버리도록 유도한다.
	sibling := root + "-evil"
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sibling) })

	file := filepath.Join(root, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	c := Config{AllowedRoots: []string{root}}

	t.Run("루트 자신은 허용", func(t *testing.T) {
		if _, err := c.ResolveDir(root); err != nil {
			t.Fatalf("허용해야 하는데 거부됐다: %v", err)
		}
	})

	t.Run("루트 아래는 허용", func(t *testing.T) {
		got, err := c.ResolveDir(inside)
		if err != nil {
			t.Fatalf("허용해야 하는데 거부됐다: %v", err)
		}
		want, _ := filepath.EvalSymlinks(inside)
		if got != want {
			t.Fatalf("경로가 다르다: got=%s want=%s", got, want)
		}
	})

	t.Run("상위로 나가면 거부", func(t *testing.T) {
		if _, err := c.ResolveDir(filepath.Dir(root)); err == nil {
			t.Fatal("거부해야 하는데 통과했다")
		}
	})

	t.Run("점점점으로 나가면 거부", func(t *testing.T) {
		if _, err := c.ResolveDir(filepath.Join(inside, "..", "..")); err == nil {
			t.Fatal("거부해야 하는데 통과했다")
		}
	})

	// 문자열 앞자리로 비교하면 여기서 뚫린다.
	t.Run("이름이 겹치는 형제는 거부", func(t *testing.T) {
		if _, err := c.ResolveDir(sibling); err == nil {
			t.Fatal("거부해야 하는데 통과했다 — 경로 요소 단위로 비교하지 않는다")
		}
	})

	t.Run("파일은 거부", func(t *testing.T) {
		if _, err := c.ResolveDir(file); err == nil {
			t.Fatal("디렉터리가 아닌데 통과했다")
		}
	})

	t.Run("없는 경로는 거부", func(t *testing.T) {
		if _, err := c.ResolveDir(filepath.Join(root, "nope")); err == nil {
			t.Fatal("없는 경로인데 통과했다")
		}
	})

	t.Run("빈 값이면 첫 루트", func(t *testing.T) {
		got, err := c.ResolveDir("")
		if err != nil {
			t.Fatal(err)
		}
		want, _ := filepath.EvalSymlinks(root)
		if got != want {
			t.Fatalf("got=%s want=%s", got, want)
		}
	})
}

// 심볼릭 링크로 루트 밖을 가리켜도 막혀야 한다.
// EvalSymlinks 없이 짜면 이 테스트가 잡는다.
func TestResolveDirSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("윈도우는 심볼릭 링크에 권한이 필요하다")
	}

	root := t.TempDir()
	outside := t.TempDir()

	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}

	c := Config{AllowedRoots: []string{root}}
	if _, err := c.ResolveDir(link); err == nil {
		t.Fatal("링크로 루트 밖에 나갔는데 통과했다")
	}
}
