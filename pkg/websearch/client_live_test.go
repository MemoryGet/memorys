package websearch

import (
	"context"
	"os"
	"testing"
)

// Run explicitly with DEEPSEEK_LIVE_TEST=1. It is skipped during ordinary
// local/CI runs because Search and Fetch are external, account-scoped calls.
func TestLiveSearchAndFetch(t *testing.T) {
	if os.Getenv("DEEPSEEK_LIVE_TEST") != "1" {
		t.Skip("set DEEPSEEK_LIVE_TEST=1 to call DeepSeek")
	}
	client, err := NewClientFromEnv(WithMaxRetries(0))
	if err != nil {
		t.Fatal(err)
	}
	search, err := client.Search(context.Background(), SearchRequest{Query: "DeepSeek API", Count: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(search.Results) == 0 || search.Results[0].URL == "" {
		t.Fatal("live search returned no usable result")
	}
	page, err := client.Fetch(context.Background(), FetchRequest{URL: search.Results[0].URL})
	if err != nil {
		t.Fatal(err)
	}
	if page.Title == "" || page.Content == "" {
		t.Fatalf("live fetch returned incomplete page: title=%q content_length=%d", page.Title, len(page.Content))
	}
	t.Logf("live search/fetch passed: title=%q content_length=%d", page.Title, len(page.Content))
}
