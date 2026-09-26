// Package secretenv는 셸과 명령에 넘길 환경변수에서 이 서비스의 비밀값을 뺀다.
//
// .env를 읽으면 TERMINAL_API_KEY·RELAY_TERMINAL_TOKEN이 이 프로세스의
// 환경에 들어간다. 그대로 물려주면 터미널에서 돌리는 모든 프로그램
// (npm 설치 스크립트, 그 터미널에서 띄운 도구 등)이 키를 읽는다.
// 셸을 쓰는 사람에게는 필요 없는 값이다.
package secretenv

import (
	"os"
	"strings"
)

// 이 접두어로 시작하는 변수는 이 서비스와 relay의 설정이다.
var privatePrefixes = []string{"TERMINAL_", "RELAY_"}

// Environ은 os.Environ()에서 비밀값을 뺀 것을 돌려준다.
func Environ() []string {
	return Filter(os.Environ())
}

// Filter는 주어진 환경에서 비밀값을 뺀다.
func Filter(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if isPrivate(name) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func isPrivate(name string) bool {
	for _, p := range privatePrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}
