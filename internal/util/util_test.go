package util

import (
	"testing"

	"mlib.com/xmq/internal/test"
)

func BenchmarkUniqRands5of5(b *testing.B) {
	for i := 0; i < b.N; i++ {
		UniqRands(5, 5)
	}
}
func BenchmarkUniqRands20of20(b *testing.B) {
	for i := 0; i < b.N; i++ {
		UniqRands(20, 20)
	}
}

func BenchmarkUniqRands20of50(b *testing.B) {
	for i := 0; i < b.N; i++ {
		UniqRands(20, 50)
	}
}

func TestUniqRands(t *testing.T) {
	var x []int
	x = UniqRands(3, 10)
	test.Equal(t, 3, len(x))

	x = UniqRands(10, 5)
	test.Equal(t, 5, len(x))

	x = UniqRands(10, 20)
	test.Equal(t, 10, len(x))
}

func TestTypeOfAddr(t *testing.T) {
	var x string
	x = TypeOfAddr("127.0.0.1:80")
	test.Equal(t, "tcp", x)

	x = TypeOfAddr("test:80")
	test.Equal(t, "tcp", x)

	x = TypeOfAddr("/var/run/xmqd.sock")
	test.Equal(t, "unix", x)

	x = TypeOfAddr("[::1%lo0]:80")
	test.Equal(t, "tcp", x)
}
