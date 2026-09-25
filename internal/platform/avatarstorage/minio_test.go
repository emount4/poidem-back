package avatarstorage

import (
	"context"
	"io"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/emount4/poidem-back/internal/platform/config"
)

func TestMinIOIntegration(t *testing.T) {
	endpoint := os.Getenv("S3_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("S3_TEST_ENDPOINT is not set")
	}
	useSSL, _ := strconv.ParseBool(os.Getenv("S3_TEST_USE_SSL"))
	cfg := config.Storage{
		Endpoint: endpoint, PublicURL: os.Getenv("S3_TEST_PUBLIC_URL"),
		AccessKey: os.Getenv("S3_TEST_ACCESS_KEY"), SecretKey: os.Getenv("S3_TEST_SECRET_KEY"),
		Bucket: os.Getenv("S3_TEST_BUCKET"), Region: "us-east-1", UseSSL: useSSL,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	storage, err := NewMinIO(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	key := "integration/avatar-" + strconv.FormatInt(time.Now().UnixNano(), 10) + ".png"
	t.Cleanup(func() { _ = storage.Delete(context.Background(), key) })
	data := []byte("\x89PNG\r\n\x1a\nminio-integration")
	url, err := storage.Put(ctx, key, data, "image/png")
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	actual, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || string(actual) != string(data) || response.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("status=%d type=%q body=%q", response.StatusCode, response.Header.Get("Content-Type"), actual)
	}
}
