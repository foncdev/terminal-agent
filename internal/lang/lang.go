// Package lang은 사용자에게 보이는 글의 언어를 고른다.
//
// relay-service와 같은 변수(RELAY_LANG)를 읽는다. en으로 시작하면 영어,
// 그 밖은 한국어(기본)다. 폰·안경·웹이 같은 글을 보므로 relay-service와
// 폰 앱의 언어와 맞춰 둔다.
//
// 값은 부를 때마다 환경에서 읽는다. .env를 읽기 전에 만든 패키지 변수
// (오류 값 등)도 나중에 글을 꺼낼 때 맞는 언어가 나오게 하려는 것이다.
//
// 운영자가 보는 로그는 옮기지 않는다(relay-service와 같다).
package lang

import (
	"os"
	"strings"
	"sync"
)

type Lang string

const (
	KO Lang = "ko"
	EN Lang = "en"
)

var (
	mu       sync.RWMutex
	override Lang
)

// Parse는 RELAY_LANG 값을 해석한다. en으로 시작하면 영어, 그 밖은 한국어.
func Parse(v string) Lang {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(v)), "en") {
		return EN
	}
	return KO
}

// Current는 지금 언어다.
func Current() Lang {
	mu.RLock()
	o := override
	mu.RUnlock()
	if o != "" {
		return o
	}
	return Parse(os.Getenv("RELAY_LANG"))
}

// Set은 환경과 상관없이 언어를 정한다. 테스트에서 쓴다.
// 돌려준 함수를 부르면 이전 상태로 돌아간다. 빈 값은 환경을 따르게 한다.
func Set(l Lang) (restore func()) {
	mu.Lock()
	prev := override
	override = l
	mu.Unlock()
	return func() {
		mu.Lock()
		override = prev
		mu.Unlock()
	}
}

// L은 지금 언어의 글을 고른다.
func L(ko, en string) string {
	if Current() == EN {
		return en
	}
	return ko
}

// Error는 꺼낼 때 언어를 고르는 오류다. 패키지 변수로 둔 오류도
// errors.Is로 비교할 수 있고, 글은 그때의 언어로 나온다.
type Error struct{ ko, en string }

func NewError(ko, en string) *Error { return &Error{ko: ko, en: en} }

func (e *Error) Error() string { return L(e.ko, e.en) }
