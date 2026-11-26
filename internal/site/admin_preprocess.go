package site

import (
	"encoding/csv"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/net/html"
)

// extractAdminAnchor parses markup as data. Nothing from the import is rendered
// as HTML, fetched from the network, or interpreted as script.
func extractAdminAnchor(raw string) (string, string) {
	z := html.NewTokenizer(strings.NewReader(raw))
	inAnchor := false
	href := ""
	var label strings.Builder
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return href, strings.TrimSpace(label.String())
		case html.StartTagToken:
			t := z.Token()
			if t.Data == "a" && !inAnchor {
				inAnchor = true
				for _, a := range t.Attr {
					if a.Key == "href" {
						href = a.Val
					}
				}
			}
		case html.TextToken:
			if inAnchor {
				label.Write(z.Text())
			}
		case html.EndTagToken:
			if z.Token().Data == "a" && inAnchor {
				return href, strings.TrimSpace(label.String())
			}
		}
	}
}

func (a *adminServer) preprocess(c *gin.Context) {
	var b struct {
		Raw     string `json:"raw_text"`
		Options struct {
			Delimiter  string         `json:"delimiter"`
			HasHeader  bool           `json:"has_header"`
			Mapping    map[string]int `json:"mapping"`
			Defaults   map[string]any `json:"defaults"`
			SourceFile string         `json:"source_file"`
		} `json:"options"`
	}
	if !adminDecode(c, &b) {
		return
	}
	if validateText(b.Options.SourceFile, 160) != nil {
		adminError(c, 422, "invalid source_file")
		return
	}
	delim := ','
	switch b.Options.Delimiter {
	case "", "auto":
		best := -1
		sample := strings.SplitN(b.Raw, "\n", 2)[0]
		for _, candidate := range []rune{'\t', '|', ',', ';'} {
			if count := strings.Count(sample, string(candidate)); count > best {
				best = count
				delim = candidate
			}
		}
	case "tab", "\\t":
		delim = '\t'
	case ",", "|", ";":
		delim = []rune(b.Options.Delimiter)[0]
	default:
		adminError(c, 422, "unsupported delimiter")
		return
	}
	preview := []gin.H{}
	errors := []gin.H{}
	normalized := []string{}
	rowCount := 0
	for n, line := range strings.Split(strings.ReplaceAll(b.Raw, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		rowCount++
		if rowCount > 10000 {
			adminError(c, 422, "preview is limited to 10000 rows")
			return
		}
		if b.Options.HasHeader {
			b.Options.HasHeader = false
			continue
		}
		fail := func(err error) { errors = append(errors, gin.H{"line": n + 1, "message": err.Error()}) }
		r := csv.NewReader(strings.NewReader(line))
		r.Comma = delim
		r.FieldsPerRecord = -1
		r.TrimLeadingSpace = true
		columns, err := r.Read()
		if err != nil {
			if href, label := extractAdminAnchor(line); href != "" {
				columns = []string{href, label}
			} else {
				fail(fmt.Errorf("invalid delimited row"))
				continue
			}
		}
		value := func(key string) string {
			index := b.Options.Mapping[key]
			if index > 0 && index <= len(columns) {
				return strings.TrimSpace(columns[index-1])
			}
			if v, ok := b.Options.Defaults[key]; ok && v != nil {
				return fmt.Sprint(v)
			}
			return ""
		}
		in := adminLinkInput{Domain: value("domain"), Language: value("lang"), AnchorText: value("anchor_text"), TargetURL: value("target_url"), Category: value("category"), Remark: value("remark")}
		for _, column := range columns {
			if href, label := extractAdminAnchor(column); href != "" {
				if in.TargetURL == "" {
					in.TargetURL = href
				}
				if in.AnchorText == "" {
					in.AnchorText = label
				}
				break
			}
		}
		if in.TargetURL == "" {
			for i, column := range columns {
				column = strings.TrimSpace(column)
				if strings.HasPrefix(column, "https://") || strings.HasPrefix(column, "http://") {
					in.TargetURL = column
					if in.AnchorText == "" && i+1 < len(columns) {
						in.AnchorText = strings.TrimSpace(columns[i+1])
					}
					break
				}
			}
		}
		if in.TargetURL == "" && in.Domain != "" {
			in.TargetURL = "https://" + in.Domain + "/"
		}
		if in.Domain == "" {
			if u, e := url.Parse(in.TargetURL); e == nil {
				in.Domain = u.Hostname()
			}
		}
		if raw := value("weight"); raw != "" {
			v, e := strconv.Atoi(raw)
			if e != nil {
				fail(fmt.Errorf("invalid weight"))
				continue
			}
			in.Weight = &v
		}
		if raw := value("enabled"); raw != "" {
			v, e := strconv.ParseBool(raw)
			if e != nil {
				fail(fmt.Errorf("invalid enabled"))
				continue
			}
			in.Enabled = &v
		}
		if raw := value("recommend_tier"); raw != "" {
			v, e := strconv.Atoi(raw)
			if e != nil || v < 0 {
				fail(fmt.Errorf("invalid recommend_tier"))
				continue
			}
			in.RecommendTier = &v
		}
		link, _, err := a.normalizeLink(in, "external")
		if err != nil {
			fail(err)
			continue
		}
		var out strings.Builder
		writer := csv.NewWriter(&out)
		writer.Comma = '|'
		_ = writer.Write([]string{link.Domain, link.Language, link.AnchorText, link.TargetURL, link.Category, strconv.Itoa(link.Weight), strconv.FormatBool(link.Enabled), link.Remark, strconv.Itoa(link.RecommendTier)})
		writer.Flush()
		normalized = append(normalized, strings.TrimSuffix(out.String(), "\n"))
		if len(preview) < 20 {
			row := adminLinkJSON(link)
			row["line"] = n + 1
			row["source_file"] = b.Options.SourceFile
			preview = append(preview, row)
		}
	}
	detected := string(delim)
	if delim == '\t' {
		detected = "tab"
	}
	adminOK(c, gin.H{"import_format": "normalized", "detected_delimiter": detected, "success_rows": len(normalized), "failed_rows": len(errors), "normalized_text": strings.Join(normalized, "\n"), "preview": preview, "errors": errors[:min(50, len(errors))]})
}
