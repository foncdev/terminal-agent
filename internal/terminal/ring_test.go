package terminal

import (
	"bytes"
	"math/rand"
	"testing"
)

func TestRingBuffer(t *testing.T) {
	tests := []struct {
		name   string
		size   int
		writes []string
		want   string
	}{
		{"안 채웠으면 그대로", 10, []string{"abc"}, "abc"},
		{"딱 맞게 채움", 5, []string{"abcde"}, "abcde"},
		{"넘치면 뒤만 남음", 5, []string{"abcdefgh"}, "defgh"},
		{"여러 번 나눠 써도 이어짐", 10, []string{"abc", "def"}, "abcdef"},
		{"한 바퀴 돌면 앞이 밀림", 5, []string{"abc", "def"}, "bcdef"},
		{"한 번에 버퍼보다 큰 입력", 4, []string{"xy", "abcdefgh"}, "efgh"},
		{"빈 입력은 무시", 5, []string{"ab", "", "c"}, "abc"},
		{"정확히 두 바퀴", 3, []string{"abcdef"}, "def"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRingBuffer(tc.size)
			for _, w := range tc.writes {
				r.write([]byte(w))
			}
			if got := string(r.bytes()); got != tc.want {
				t.Fatalf("got=%q want=%q", got, tc.want)
			}
		})
	}
}

// 무작위로 써넣어도 늘 "마지막 N바이트"와 같아야 한다.
// 경계 처리가 틀리면 여기서 깨진다.
func TestRingBufferMatchesTail(t *testing.T) {
	const size = 64
	r := newRingBuffer(size)
	var all []byte

	rnd := rand.New(rand.NewSource(1))
	for i := 0; i < 500; i++ {
		chunk := make([]byte, rnd.Intn(100))
		for j := range chunk {
			chunk[j] = byte('a' + rnd.Intn(26))
		}
		r.write(chunk)
		all = append(all, chunk...)

		want := all
		if len(want) > size {
			want = want[len(want)-size:]
		}
		if got := r.bytes(); !bytes.Equal(got, want) {
			t.Fatalf("%d번째: got=%q want=%q", i, got, want)
		}
	}
}

func TestRingBufferZeroSize(t *testing.T) {
	// 0을 줘도 터지면 안 된다.
	r := newRingBuffer(0)
	r.write([]byte("abc"))
	if got := r.bytes(); len(got) != 1 {
		t.Fatalf("1바이트만 남아야 한다: got=%q", got)
	}
}
