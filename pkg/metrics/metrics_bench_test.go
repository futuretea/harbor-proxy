package metrics

import (
	"testing"
	"time"
)

// BenchmarkRecordRequest measures the overhead of recording metrics
func BenchmarkRecordRequest(b *testing.B) {
	method := "GET"
	path := "/v2/test/repo/blobs/sha256:abc123"
	host := "registry-test.example.com:8099"
	status := 200
	duration := 100 * time.Millisecond
	bytesReceived := int64(1024)
	bytesSent := int64(1024 * 1024) // 1MB

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		RecordRequest(method, path, host, status, duration, bytesReceived, bytesSent)
	}
}

// BenchmarkRecordRequestParallel measures concurrent overhead
func BenchmarkRecordRequestParallel(b *testing.B) {
	method := "GET"
	path := "/v2/test/repo/blobs/sha256:abc123"
	host := "registry-test.example.com:8099"
	status := 200
	duration := 100 * time.Millisecond
	bytesReceived := int64(1024)
	bytesSent := int64(1024 * 1024)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			RecordRequest(method, path, host, status, duration, bytesReceived, bytesSent)
		}
	})
}

// BenchmarkActiveConnections measures gauge operations
func BenchmarkActiveConnections(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ActiveConnections.Inc()
		ActiveConnections.Dec()
	}
}

// BenchmarkPathType measures path classification overhead
func BenchmarkPathType(b *testing.B) {
	paths := []string{
		"/v2/",
		"/healthz",
		"/v2/test/repo/manifests/latest",
		"/v2/test/repo/blobs/sha256:abc123",
		"/service/token",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, path := range paths {
			_ = PathType(path)
		}
	}
}
