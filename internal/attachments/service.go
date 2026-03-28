package attachments

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"gopher-ai/internal/chat"
	"gopher-ai/internal/ids"
)

var ErrAttachmentNotFound = errors.New("attachment not found")

type Validator struct {
	MaxSize           int64
	AllowedMimeTypes  []string
	BlockedExtensions map[string]struct{}
	BlockedMimeTypes  map[string]struct{}
}

type Service struct {
	filesDir    string
	metadataDir string
	validator   Validator
}

func NewService(filesDir, metadataDir string, maxSize int64) *Service {
	return &Service{
		filesDir:    filesDir,
		metadataDir: metadataDir,
		validator:   DefaultValidator(maxSize),
	}
}

func DefaultValidator(maxSize int64) Validator {
	blockedExtensions := map[string]struct{}{
		"exe": {}, "dll": {}, "so": {}, "dylib": {}, "bin": {}, "com": {}, "msi": {},
		"sh": {}, "bash": {}, "zsh": {}, "cmd": {}, "bat": {}, "ps1": {}, "scr": {},
		"vbs": {}, "js": {}, "jar": {}, "app": {},
	}

	blockedMimeTypes := map[string]struct{}{
		"application/x-msdownload": {},
		"application/x-dosexec":    {},
		"application/x-executable": {},
		"application/x-sh":         {},
		"text/x-shellscript":       {},
	}

	return Validator{
		MaxSize: maxSize,
		AllowedMimeTypes: []string{
			"image/*",
			"text/*",
			"application/json",
			"application/pdf",
			"application/zip",
		},
		BlockedExtensions: blockedExtensions,
		BlockedMimeTypes:  blockedMimeTypes,
	}
}

func (v Validator) Validate(filename string, size int64, mimeType string) error {
	if size > v.MaxSize {
		return fmt.Errorf("file too large: %d > %d bytes", size, v.MaxSize)
	}

	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(filename)), ".")
	if _, blocked := v.BlockedExtensions[ext]; blocked {
		return fmt.Errorf("file extension not allowed: .%s", ext)
	}

	if _, blocked := v.BlockedMimeTypes[strings.ToLower(mimeType)]; blocked {
		return fmt.Errorf("mime type not allowed: %s", mimeType)
	}

	for _, allowed := range v.AllowedMimeTypes {
		if matchesMimeType(strings.ToLower(mimeType), strings.ToLower(allowed)) {
			return nil
		}
	}

	return fmt.Errorf("mime type not allowed: %s", mimeType)
}

func (s *Service) SaveUpload(ctx context.Context, header *multipart.FileHeader) (chat.AttachmentRef, error) {
	if err := ctx.Err(); err != nil {
		return chat.AttachmentRef{}, err
	}

	file, err := header.Open()
	if err != nil {
		return chat.AttachmentRef{}, err
	}
	defer file.Close()

	sniff := make([]byte, 512)
	readBytes, err := file.Read(sniff)
	if err != nil && !errors.Is(err, io.EOF) {
		return chat.AttachmentRef{}, err
	}

	mimeType := http.DetectContentType(sniff[:readBytes])
	if err := s.validator.Validate(header.Filename, header.Size, mimeType); err != nil {
		return chat.AttachmentRef{}, err
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return chat.AttachmentRef{}, err
	}

	if err := os.MkdirAll(s.filesDir, 0o755); err != nil {
		return chat.AttachmentRef{}, err
	}
	if err := os.MkdirAll(s.metadataDir, 0o755); err != nil {
		return chat.AttachmentRef{}, err
	}

	attachmentID := ids.New("att")
	filename := sanitizeFilename(header.Filename)
	storedPath := filepath.Join(s.filesDir, attachmentID+"_"+filename)

	destination, err := os.Create(storedPath)
	if err != nil {
		return chat.AttachmentRef{}, err
	}

	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(destination, hash), file)
	closeErr := destination.Close()
	if copyErr != nil {
		return chat.AttachmentRef{}, copyErr
	}
	if closeErr != nil {
		return chat.AttachmentRef{}, closeErr
	}

	attachment := chat.AttachmentRef{
		ID:        attachmentID,
		Filename:  header.Filename,
		Size:      written,
		MimeType:  mimeType,
		Hash:      hex.EncodeToString(hash.Sum(nil)),
		LocalPath: storedPath,
	}

	preview, err := buildPreview(storedPath, mimeType)
	if err == nil {
		attachment.Preview = preview
	}

	if err := s.saveMetadata(attachment); err != nil {
		return chat.AttachmentRef{}, err
	}

	return attachment, nil
}

func (s *Service) Get(id string) (chat.AttachmentRef, error) {
	path := filepath.Join(s.metadataDir, id+".json")
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return chat.AttachmentRef{}, ErrAttachmentNotFound
		}
		return chat.AttachmentRef{}, err
	}
	defer file.Close()

	var attachment chat.AttachmentRef
	if err := json.NewDecoder(file).Decode(&attachment); err != nil {
		return chat.AttachmentRef{}, err
	}

	return attachment, nil
}

func (s *Service) Delete(id string) error {
	attachment, err := s.Get(id)
	if err != nil {
		return err
	}

	if err := os.Remove(filepath.Join(s.metadataDir, id+".json")); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := os.Remove(attachment.LocalPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	return nil
}

func (s *Service) saveMetadata(attachment chat.AttachmentRef) error {
	data, err := json.MarshalIndent(attachment, "", "  ")
	if err != nil {
		return err
	}

	path := filepath.Join(s.metadataDir, attachment.ID+".json")
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, data, 0o644); err != nil {
		return err
	}

	return os.Rename(tempPath, path)
}

func matchesMimeType(candidate, allowed string) bool {
	if strings.HasSuffix(allowed, "/*") {
		return strings.HasPrefix(candidate, strings.TrimSuffix(allowed, "*"))
	}
	return candidate == allowed
}

func sanitizeFilename(filename string) string {
	base := filepath.Base(filename)
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '.', r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}, base)
}

func buildPreview(path, mimeType string) (string, error) {
	switch {
	case strings.HasPrefix(mimeType, "text/") || mimeType == "application/json":
		file, err := os.Open(path)
		if err != nil {
			return "", err
		}
		defer file.Close()

		data, err := io.ReadAll(io.LimitReader(file, 2_048))
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(data)), nil
	case mimeType == "application/pdf":
		return "PDF uploaded. Text extraction is not enabled in the MVP yet.", nil
	default:
		return "", nil
	}
}
