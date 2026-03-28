package attachments

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestValidatorRejectsExecutable(t *testing.T) {
	t.Parallel()

	validator := DefaultValidator(1024)
	if err := validator.Validate("danger.exe", 10, "application/octet-stream"); err == nil {
		t.Fatal("expected executable to be rejected")
	}
}

func TestServiceSavesTextAttachment(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	filesDir := filepath.Join(root, "files")
	metaDir := filepath.Join(root, "meta")
	service := NewService(filesDir, metaDir, 1024)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "notes.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("hello from gopher ai")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/api/attachments/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if err := req.ParseMultipartForm(2048); err != nil {
		t.Fatal(err)
	}

	header := req.MultipartForm.File["file"][0]
	attachment, err := service.SaveUpload(context.Background(), header)
	if err != nil {
		t.Fatalf("save upload: %v", err)
	}

	if attachment.Filename != "notes.txt" {
		t.Fatalf("unexpected filename: %s", attachment.Filename)
	}
	if attachment.Preview == "" {
		t.Fatal("expected preview to be populated for text attachment")
	}
	if _, err := os.Stat(attachment.LocalPath); err != nil {
		t.Fatalf("expected uploaded file to exist: %v", err)
	}
}
