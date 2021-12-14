package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/tomnomnom/linkheader"
)

func dataSource() *schema.Resource {
	return &schema.Resource{
		ReadContext: dataSourceRead,

		Schema: map[string]*schema.Schema{
			"base_url": {
				Type:     schema.TypeString,
				Required: true,
				Elem: &schema.Schema{
					Type: schema.TypeString,
				},
			},

			"token": {
				Type:     schema.TypeString,
				Required: true,
				Elem: &schema.Schema{
					Type: schema.TypeString,
				},
			},

			"course_code": {
				Type:     schema.TypeInt,
				Required: true,
				Elem: &schema.Schema{
					Type: schema.TypeInt,
				},
			},

			"body": {
				Type:     schema.TypeString,
				Computed: true,
				Elem: &schema.Schema{
					Type: schema.TypeString,
				},
			},
		},
	}
}

func dataSourceRead(ctx context.Context, d *schema.ResourceData, meta interface{}) (diags diag.Diagnostics) {
	baseUrl := d.Get("base_url").(string)
	token := d.Get("token").(string)
	courseCode := strconv.Itoa(d.Get("course_code").(int))

	client := &http.Client{}

	req, err := http.NewRequestWithContext(ctx, "GET", baseUrl+"/api/v1/courses/"+courseCode+"/users/", nil)
	if err != nil {
		return append(diags, diag.Errorf("Error creating request: %s", err)...)
	}

	req.Header.Set("Authorization", "Bearer "+token)

	bytes, err := recursiveDo(client, req)
	if err != nil {
		return append(diags, diag.Errorf("Error making request: %s", err)...)
	}

	d.Set("body", string(bytes))

	// set ID as something more stable than time
	d.SetId(courseCode)

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

func recursiveDo(client *http.Client, req *http.Request) ([]byte, error) {

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

	bytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var users []User
	err = json.Unmarshal(bytes, &users)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling json: %s", err)
	}
	results, err := json.Marshal(users)
	if err != nil {
		return nil, fmt.Errorf("error marshaling json: %s", err)
	}

	links := linkheader.Parse(resp.Header.Get("Link")).FilterByRel("next")
	if len(links) != 0 {
		url, err := url.Parse(links[0].URL)
		if err != nil {
			return nil, err
		}
		req.URL.RawQuery = url.RawQuery
		//recReq, err := http.NewRequestWithContext(req.Context(), "GET", links[0].URL, nil)
		if err != nil {
			return nil, err
		}
		recurse, err := recursiveDo(client, req)
		if err != nil {
			return nil, err
		}
		results = append(results[:len(results)-1], ',')
		results = append(results, recurse[1:]...)
	}

	return results, nil
}
