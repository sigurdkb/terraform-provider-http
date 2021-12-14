package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"mime"
	"net/http"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/tomnomnom/linkheader"
)

func dataSource() *schema.Resource {
	return &schema.Resource{
		ReadContext: dataSourceRead,

		Schema: map[string]*schema.Schema{
			"url": {
				Type:     schema.TypeString,
				Required: true,
				Elem: &schema.Schema{
					Type: schema.TypeString,
				},
			},

			"request_headers": {
				Type:     schema.TypeMap,
				Optional: true,
				Elem: &schema.Schema{
					Type: schema.TypeString,
				},
			},

			"body": {
				Type:     schema.TypeString,
				Computed: true,
				Elem: &schema.Schema{
					Type: schema.TypeString,
				},
			},

			// "response_headers": {
			// 	Type:     schema.TypeMap,
			// 	Computed: true,
			// 	Elem: &schema.Schema{
			// 		Type: schema.TypeString,
			// 	},
			// },
		},
	}
}

func dataSourceRead(ctx context.Context, d *schema.ResourceData, meta interface{}) (diags diag.Diagnostics) {
	url := d.Get("url").(string)
	headers := d.Get("request_headers").(map[string]interface{})

	client := &http.Client{}

	bytes, err := recursiveGetWithContext(client, ctx, url, headers)
	if err != nil {
		return append(diags, diag.Errorf("Error making request: %s", err)...)
	}

	// responseHeaders := make(map[string]string)
	// for k, v := range resp.Header {
	// 	// Concatenate according to RFC2616
	// 	// cf. https://www.w3.org/Protocols/rfc2616/rfc2616-sec4.html#sec4.2
	// 	responseHeaders[k] = strings.Join(v, ", ")
	// }
	var users []User
	err = json.Unmarshal(bytes, &users)
	if err != nil {
		return append(diags, diag.Errorf("Error filtering json: %s", err)...)
	}
	result, err := json.Marshal(users)
	if err != nil {
		return append(diags, diag.Errorf("Error filtering json: %s", err)...)
	}

	d.Set("body", string(result))
	// if err = d.Set("response_headers", responseHeaders); err != nil {
	// 	return append(diags, diag.Errorf("Error setting HTTP response headers: %s", err)...)
	// }

	// set ID as something more stable than time
	d.SetId(url)

	return diags
}

// This is to prevent potential issues w/ binary files
// and generally unprintable characters
// See https://github.com/hashicorp/terraform/pull/3858#issuecomment-156856738
func isContentTypeText(contentType string) bool {

	parsedType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}

	allowedContentTypes := []*regexp.Regexp{
		regexp.MustCompile("^text/.+"),
		regexp.MustCompile("^application/json$"),
		regexp.MustCompile("^application/samlmetadata\\+xml"),
	}

	for _, r := range allowedContentTypes {
		if r.MatchString(parsedType) {
			charset := strings.ToLower(params["charset"])
			return charset == "" || charset == "utf-8" || charset == "us-ascii"
		}
	}

	return false
}

func recursiveGetWithContext(client *http.Client, ctx context.Context, url string, headers map[string]interface{}) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	for name, value := range headers {
		req.Header.Set(name, value.(string))
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP request error. Response code: %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if contentType == "" || !isContentTypeText(contentType) {
		return nil, fmt.Errorf("Content-Type is not recognized as a text type, got %q", contentType)
	}

	results, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	links := linkheader.Parse(resp.Header.Get("Link")).FilterByRel("next")
	if len(links) != 0 {
		recurse, err := recursiveGetWithContext(client, ctx, links[0].URL, headers)
		if err != nil {
			return nil, err
		}
		results = append(results[:len(results)-1], ',')
		results = append(results, recurse[1:]...)
	}

	return results, nil
}
