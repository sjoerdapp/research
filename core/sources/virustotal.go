package sources

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/subfinder/research/core"
	"golang.org/x/sync/semaphore"
)

// Virustotal is a source to process subdomains from https://Virustotal.com
type Virustotal struct {
	APIToken string
	lock     *semaphore.Weighted
}

type virustotalapiObject struct {
	Subdomains []string `json:"subdomains"`
}

// ProcessDomain takes a given base domain and attempts to enumerate subdomains.
func (source *Virustotal) ProcessDomain(ctx context.Context, domain string) <-chan *core.Result {
	if source.lock == nil {
		source.lock = defaultLockValue()
	}

	var resultLabel = "virustotal"

	results := make(chan *core.Result)
	go func(domain string, results chan *core.Result) {
		defer close(results)

		if err := source.lock.Acquire(ctx, 1); err != nil {
			sendResultWithContext(ctx, results, core.NewResult(resultLabel, nil, err))
			return
		}

		domainExtractor, err := core.NewSubdomainExtractor(domain)
		if err != nil {
			sendResultWithContext(ctx, results, core.NewResult(resultLabel, nil, err))
			return
		}

		var req *http.Request

		if source.APIToken == "" {
			req, err = http.NewRequest(http.MethodGet, "https://www.virustotal.com/en/domain/"+domain+"/information/", nil)
		} else {
			req, err = http.NewRequest(http.MethodGet, "https://www.virustotal.com/vtapi/v2/domain/report?apikey="+source.APIToken+"&domain="+domain, nil)
		}

		if err != nil {
			sendResultWithContext(ctx, results, core.NewResult(resultLabel, nil, err))
			return
		}

		req.Cancel = ctx.Done()
		req.WithContext(ctx)

		resp, err := core.HTTPClient.Do(req)
		if err != nil {
			sendResultWithContext(ctx, results, core.NewResult(resultLabel, nil, err))
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			sendResultWithContext(ctx, results, core.NewResult(resultLabel, nil, errors.New(resp.Status)))
			return
		}

		if source.APIToken == "" {
			scanner := bufio.NewScanner(resp.Body)
			for scanner.Scan() {
				if ctx.Err() != nil {
					return
				}
				for _, str := range domainExtractor.FindAllString(scanner.Text(), -1) {
					if !sendResultWithContext(ctx, results, core.NewResult(resultLabel, str, nil)) {
						return
					}
				}
			}

			err = scanner.Err()

			if err != nil {
				sendResultWithContext(ctx, results, core.NewResult(resultLabel, nil, err))
				return
			}
		} else {
			hostResponse := virustotalapiObject{}

			err = json.NewDecoder(resp.Body).Decode(&hostResponse)
			if err != nil {
				sendResultWithContext(ctx, results, core.NewResult(resultLabel, nil, err))
				return
			}

			for _, sub := range hostResponse.Subdomains {
				str := sub + "." + domain
				if !sendResultWithContext(ctx, results, core.NewResult(resultLabel, str, nil)) {
					return
				}
			}
		}

	}(domain, results)
	return results
}
