package mail

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// DeleteTag deletes every message tagged tag: one app's inbox, since apps
// send with MAIL_USERNAME set to their APP_NAME.
func (t Target) DeleteTag(ctx context.Context, tag string) error {
	query := url.Values{"query": {fmt.Sprintf("tag:%q", tag)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, "http://"+t.Addr+"/api/v1/search?"+query.Encode(), nil)
	if err != nil {
		return err
	}
	if t.Auth != "" {
		req.Header.Set("Authorization", t.authHeader())
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("asking mail: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("mail answered %s deleting the %s inbox", resp.Status, tag)
	}
	return nil
}
