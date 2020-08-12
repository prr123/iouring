package fixed

import (
	"encoding/binary"
	"io/ioutil"
	"os"
	"sync"
	"syscall"
	"testing"

	"github.com/dshulyak/uring"
	"github.com/dshulyak/uring/queue"
	"github.com/stretchr/testify/require"
)

func TestWrite(t *testing.T) {
	queue, err := queue.SetupSharded(8, 1024, nil)
	defer queue.Close()

	f, err := ioutil.TempFile("", "test")
	require.NoError(t, err)
	defer os.Remove(f.Name())

	n := 100
	size := 10
	pool, err := New(queue, size, n)
	require.NoError(t, err)

	run := func() {
		for i := 0; i < n; i++ {
			buf := pool.Get()
			defer pool.Put(buf)
			buf.NWrite(size)

			require.NoError(t, queue.CompleteAll(func(sqe *uring.SQEntry) {
				uring.WriteFixed(sqe, f.Fd(), buf.Base(), buf.Len(), 0, 0, buf.Index())
			}, func(cqe uring.CQEntry) {
				require.Equal(t, int32(10), cqe.Result(), syscall.Errno(-cqe.Result()))
			}))
		}
	}
	// run it couple of times to test that buffers are reused correctly
	for i := 0; i < 3; i++ {
		run()
	}
}

func TestConcurrentWrites(t *testing.T) {
	queue, err := queue.SetupSharded(8, 1024, nil)
	require.NoError(t, err)
	defer queue.Close()

	f, err := ioutil.TempFile("", "test-concurrent-writes-")
	require.NoError(t, err)
	defer os.Remove(f.Name())

	var wg sync.WaitGroup
	var n int64 = 10000

	pool, err := New(queue, 8, int(n))
	require.NoError(t, err)
	for i := int64(0); i < n; i++ {
		wg.Add(1)
		go func(i uint64) {
			buf := pool.Get()
			defer pool.Put(buf)
			binary.BigEndian.PutUint64(buf.Bytes(), i)
			buf.NWrite(8)
			_, _ = queue.Complete(func(sqe *uring.SQEntry) {
				uring.WriteFixed(sqe, f.Fd(), buf.Base(), buf.Len(), i*8, 0, buf.Index())
			})
			wg.Done()
		}(uint64(i))
	}
	wg.Wait()

	buf2 := make([]byte, 8)
	for i := int64(0); i < n; i++ {
		_, err := f.ReadAt(buf2, i*8)
		require.NoError(t, err)
		rst := binary.BigEndian.Uint64(buf2[:])
		require.Equal(t, i, int64(rst))
	}
}

func BenchmarkPool(b *testing.B) {
	queue, err := queue.SetupSharded(8, 1024, nil)
	require.NoError(b, err)
	defer queue.Close()

	pool, err := New(queue, 8, 10000)
	require.NoError(b, err)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			buf := pool.Get()
			pool.Put(buf)
		}
	})
}
