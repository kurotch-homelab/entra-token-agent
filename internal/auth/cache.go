package auth

import (
	"context"
	"errors"
	"os"

	"github.com/AzureAD/microsoft-authentication-library-for-go/apps/cache"
	"github.com/kurotch-homelab/entra-token-agent/internal/securefile"
)

type FileCache struct{ Path string }

func (f FileCache) Replace(ctx context.Context, c cache.Unmarshaler, _ cache.ReplaceHints) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b, err := os.ReadFile(f.Path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return c.Unmarshal(b)
}
func (f FileCache) Export(ctx context.Context, c cache.Marshaler, _ cache.ExportHints) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	b, err := c.Marshal()
	if err != nil {
		return err
	}
	return securefile.Write(f.Path, b)
}
