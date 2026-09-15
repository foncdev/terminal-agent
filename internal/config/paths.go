package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ResolveDir는 시작 디렉터리를 확인하고 실제 경로로 바꾼다.
//
// 심볼릭 링크를 따라간 뒤에 검사하므로 링크로 빠져나갈 수 없다.
//
// 다시 말하지만 이건 시작 위치만 본다. 셸이 뜬 다음 cd로 어디를 가든
// 막지 않는다. 실수를 줄이는 장치이지 보안 경계가 아니다.
func (c Config) ResolveDir(dir string) (string, error) {
	if dir == "" {
		if len(c.AllowedRoots) == 0 {
			return os.Getwd()
		}
		dir = c.AllowedRoots[0]
	}

	real, err := filepath.EvalSymlinks(expandHome(dir))
	if err != nil {
		return "", fmt.Errorf("경로를 찾을 수 없습니다: %s", dir)
	}
	real, err = filepath.Abs(real)
	if err != nil {
		return "", err
	}

	info, err := os.Stat(real)
	if err != nil {
		return "", fmt.Errorf("경로를 찾을 수 없습니다: %s", dir)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("디렉토리가 아닙니다: %s", real)
	}

	// 루트를 안 정했으면 제한 없이 쓴다. 기본값이 홈이라 보통은 여기 안 온다.
	if len(c.AllowedRoots) == 0 {
		return real, nil
	}

	for _, root := range c.AllowedRoots {
		r, err := filepath.EvalSymlinks(root)
		if err != nil {
			// 없는 루트는 건너뛴다. 설정에 오타가 있어도 나머지는 살린다.
			continue
		}
		if r, err = filepath.Abs(r); err == nil && isInside(r, real) {
			return real, nil
		}
	}

	return "", fmt.Errorf("허용된 루트 밖의 경로입니다: %s (허용: %s)",
		real, strings.Join(c.AllowedRoots, ", "))
}

// isInside는 target이 root 아래(또는 root 자신)인지 본다.
//
// 문자열 앞자리 비교로는 /a/bc가 /a/b 안에 있다고 잘못 볼 수 있어서
// 경로 요소 단위로 끊어 본다.
func isInside(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	// ".." 로 시작하면 밖이다.
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	// 윈도우는 드라이브가 다르면 Rel이 절대경로를 준다.
	if runtime.GOOS == "windows" && filepath.IsAbs(rel) {
		return false
	}
	return true
}
