package storage

import (
	"strings"
	"testing"
	"time"
)

// TestPresignGET_AWSVector AWS 官方文档 SigV4 查询串签名示例（S3 开发者指南
// "Authenticating Requests: Using Query Parameters"）——签名器正确性的
// 权威对拍。向量：us-east-1 / examplebucket / test.txt / 20130524T000000Z。
func TestPresignGET_AWSVector(t *testing.T) {
	s := &Signer{
		accountID: "examplebucket.s3.amazonaws.com",
		bucket:    "examplebucket",
		accessKey: "AKIAIOSFODNN7EXAMPLE",
		secretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		region:    "us-east-1",
	}

	got := s.presignAt("examplebucket.s3.amazonaws.com", "test.txt", 86400*time.Second, "20130524T000000Z")
	want := "https://examplebucket.s3.amazonaws.com/test.txt" +
		"?X-Amz-Algorithm=AWS4-HMAC-SHA256" +
		"&X-Amz-Credential=AKIAIOSFODNN7EXAMPLE%2F20130524%2Fus-east-1%2Fs3%2Faws4_request" +
		"&X-Amz-Date=20130524T000000Z" +
		"&X-Amz-Expires=86400" +
		"&X-Amz-SignedHeaders=host" +
		"&X-Amz-Signature=aeeed9bbccd4d02ee5c0109b86d86835f995330da4c265957d157751f604d404"
	if got != want {
		t.Errorf("signature mismatch:\n got: %s\nwant: %s", got, want)
	}
}

// TestPresignGET_KeyEncoding 对象 key 含空格/中文等多字节字符时必须逐段编码。
func TestPresignGET_KeyEncoding(t *testing.T) {
	s := New("acct", "bucket", "ak", "sk")
	u := s.PresignGET("creatives/ab/adv-1/cr 素材.mp4", time.Hour)
	// 路径段中的空格与中文必须被编码，斜杠保留
	if !strings.Contains(u, "/creatives/ab/adv-1/cr%20%E7%B4%A0%E6%9D%90.mp4?") {
		t.Errorf("object key not segment-encoded: %s", u)
	}
	if !strings.Contains(u, "X-Amz-Signature=") {
		t.Error("missing signature param")
	}
	// 凭证中的 / 必须编码为 %2F
	if !strings.Contains(u, "X-Amz-Credential=ak%2F") {
		t.Errorf("credential slash not encoded: %s", u)
	}
}
