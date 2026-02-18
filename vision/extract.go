package vision

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

const extractPrompt = `This image shows a stock watchlist, portfolio, or list of tickers. Extract ALL stock ticker symbols (e.g. AAPL, MSFT, 005930.KS, 0700.HK, D05.SI).

Return ONLY a JSON array of strings. No other text. Example: ["AAPL","MSFT","005930.KS"]`

// ExtractSymbols uses GPT-4 Vision to extract stock ticker symbols from an image.
// Returns a deduplicated slice of symbols, or an error if extraction fails.
func ExtractSymbols(imageBytes []byte, apiKey string) ([]string, error) {
	if len(imageBytes) == 0 {
		return nil, fmt.Errorf("empty image")
	}

	// Detect MIME type (Telegram typically sends JPEG)
	mime := "image/jpeg"
	if len(imageBytes) >= 4 && imageBytes[0] == 0x89 && imageBytes[1] == 0x50 && imageBytes[2] == 0x4E {
		mime = "image/png"
	} else if len(imageBytes) >= 4 && imageBytes[0] == 0x52 && imageBytes[1] == 0x49 && imageBytes[2] == 0x46 {
		mime = "image/webp"
	}

	dataURL := "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(imageBytes)

	client := openai.NewClient(option.WithAPIKey(apiKey))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	completion, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: openai.ChatModelGPT4oMini,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.UserMessage([]openai.ChatCompletionContentPartUnionParam{
				openai.ImageContentPart(openai.ChatCompletionContentPartImageImageURLParam{
					URL:    dataURL,
					Detail: "high",
				}),
				openai.TextContentPart(extractPrompt),
			}),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("OpenAI API error: %w", err)
	}

	if len(completion.Choices) == 0 {
		return nil, fmt.Errorf("no response from OpenAI")
	}

	content := strings.TrimSpace(completion.Choices[0].Message.Content)
	if content == "" {
		return nil, fmt.Errorf("empty response from OpenAI")
	}

	// Try to parse as JSON array
	symbols, err := parseJSONArray(content)
	if err != nil {
		// Fallback: try to extract symbols with regex (e.g. "AAPL", "MSFT" patterns)
		symbols = extractSymbolsWithRegex(content)
		if len(symbols) == 0 {
			return nil, fmt.Errorf("could not extract symbols from image: %w", err)
		}
	}

	// Deduplicate and normalize
	seen := make(map[string]bool)
	result := make([]string, 0, len(symbols))
	for _, s := range symbols {
		s = strings.ToUpper(strings.TrimSpace(s))
		if s == "" || len(s) > 20 {
			continue
		}
		if seen[s] {
			continue
		}
		seen[s] = true
		result = append(result, s)
	}

	return result, nil
}

// parseJSONArray attempts to parse the response as a JSON array of strings.
func parseJSONArray(s string) ([]string, error) {
	// Handle markdown code blocks
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		lines := strings.SplitN(s, "\n", -1)
		for i, line := range lines {
			if strings.TrimSpace(line) == "```" || strings.HasPrefix(strings.TrimSpace(line), "```json") {
				lines = lines[i+1:]
				for j := len(lines) - 1; j >= 0; j-- {
					if strings.TrimSpace(lines[j]) == "```" {
						lines = lines[:j]
						break
					}
				}
				s = strings.Join(lines, "\n")
				break
			}
		}
	}

	var arr []string
	if err := json.Unmarshal([]byte(s), &arr); err != nil {
		return nil, err
	}
	return arr, nil
}

// extractSymbolsWithRegex falls back to regex when JSON parsing fails.
// Matches common ticker patterns: 1-5 letter symbols, or exchange suffixes like 005930.KS, 0700.HK.
var tickerRegex = regexp.MustCompile(`\b([A-Z]{1,5}(?:\.[A-Z]{2})?)\b`)

func extractSymbolsWithRegex(s string) []string {
	s = strings.ToUpper(s)
	matches := tickerRegex.FindAllStringSubmatch(s, -1)
	result := make([]string, 0, len(matches))
	seen := make(map[string]bool)
	for _, m := range matches {
		if len(m) < 2 {
			continue
		}
		sym := m[1]
		if len(sym) < 2 || seen[sym] {
			continue
		}
		seen[sym] = true
		result = append(result, sym)
	}
	return result
}
