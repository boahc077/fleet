package vulnerabilities

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/facebookincubator/nvdtools/cvefeed"
	feednvd "github.com/facebookincubator/nvdtools/cvefeed/nvd"
	"github.com/fleetdm/fleet/v4/pkg/download"
	"github.com/fleetdm/fleet/v4/pkg/fleethttp"
	"github.com/fleetdm/fleet/v4/server/fleet"
	"github.com/fleetdm/fleet/v4/server/ptr"
)

// Sync downloads all the vulnerability data sources.
func Sync(vulnPath string, cpeDatabaseURL string) error {
	client := fleethttp.NewClient()

	if err := DownloadCPEDatabase(vulnPath, client, WithCPEURL(cpeDatabaseURL)); err != nil {
		return fmt.Errorf("sync CPE database: %w", err)
	}

	if err := DownloadNVDCVEFeed(vulnPath, ""); err != nil {
		return fmt.Errorf("sync NVD CVE feed: %w", err)
	}

	if err := DownloadEPSSFeed(vulnPath, client); err != nil {
		return fmt.Errorf("sync EPSS CVE feed: %w", err)
	}

	if err := DownloadCISAKnownExploitsFeed(vulnPath, client); err != nil {
		return fmt.Errorf("sync CISA known exploits feed: %w", err)
	}

	return nil
}

const epssFeedsURL = "https://epss.cyentia.com"
const epssFilename = "epss_scores-current.csv.gz"

// DownloadEPSSFeed downloads the EPSS scores feed.
func DownloadEPSSFeed(vulnPath string, client *http.Client) error {
	urlString := epssFeedsURL + "/" + epssFilename
	u, err := url.Parse(urlString)
	if err != nil {
		return fmt.Errorf("parse url: %w", err)
	}
	path := filepath.Join(vulnPath, strings.TrimSuffix(epssFilename, ".gz"))

	err = download.DownloadAndExtract(client, u, path)
	if err != nil {
		return fmt.Errorf("download %s: %w", u, err)
	}

	return nil
}

// epssScore represents the EPSS score for a CVE.
type epssScore struct {
	CVE   string
	Score float64
}

func parseEPSSScoresFile(path string) ([]epssScore, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.Comment = '#'
	r.FieldsPerRecord = 3

	// skip the header
	r.Read()

	var epssScores []epssScore
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		// each row should have 3 records: cve, epss, and percentile
		if len(rec) != 3 {
			continue
		}

		cve := rec[0]
		score, err := strconv.ParseFloat(rec[1], 64)
		if err != nil {
			return nil, fmt.Errorf("parse epss score: %w", err)
		}

		// ignore percentile

		epssScores = append(epssScores, epssScore{
			CVE:   cve,
			Score: score,
		})
	}

	return epssScores, nil
}

const cisaKnownExploitsURL = "https://www.cisa.gov/sites/default/files/feeds/known_exploited_vulnerabilities.json"
const cisaKnownExploitsFilename = "known_exploited_vulnerabilities.json"

// knownExploitedVulnerabilitiesCatalog represents the CISA Catalog of Known Exploited Vulnerabilities.
type knownExploitedVulnerabilitiesCatalog struct {
	Title           string                        `json:"title"`
	CatalogVersion  string                        `json:"catalogVersion"`
	DateReleased    time.Time                     `json:"dateReleased"`
	Count           int                           `json:"count"`
	Vulnerabilities []knownExploitedVulnerability `json:"vulnerabilities"`
}

// knownExploitedVulnerability represents a known exploit in the CISA catalog.
type knownExploitedVulnerability struct {
	CVEID string `json:"cveID"`
	// remaining fields omitted
	// VendorProject     string `json:"vendorProject"`
	// Product           string `json:"product"`
	// VulnerabilityName string `json:"vulnerabilityName"`
	// DateAdded         time.time `json:"dateAdded"`
	// ShortDescription  string `json:"shortDescription"`
	// RequiredAction    string `json:"requiredAction"`
	// DueDate           time.time `json:"dueDate"`
}

// DownloadCISAKnownExploitsFeed downloads the CISA known exploited vulnerabilities feed.
func DownloadCISAKnownExploitsFeed(vulnPath string, client *http.Client) error {
	path := filepath.Join(vulnPath, cisaKnownExploitsFilename)

	u, err := url.Parse(cisaKnownExploitsURL)
	if err != nil {
		return err
	}

	err = download.Download(client, u, path)
	if err != nil {
		return fmt.Errorf("download cisa known exploits: %w", err)
	}

	return nil
}

// LoadCVEMeta loads the cvss scores, epss scores, and known exploits from the previously downloaded feeds and saves
// them to the database.
func LoadCVEMeta(vulnPath string, ds fleet.Datastore) error {
	// load cvss scores
	files, err := getNVDCVEFeedFiles(vulnPath)
	if err != nil {
		return fmt.Errorf("get nvd cve feeds: %w", err)
	}

	dict, err := cvefeed.LoadJSONDictionary(files...)
	if err != nil {
		return err
	}

	metaMap := make(map[string]fleet.CVEMeta)
	for cve := range dict {
		schema := dict[cve].(*feednvd.Vuln).Schema()
		if schema.Impact.BaseMetricV3 == nil {
			continue
		}
		baseScore := schema.Impact.BaseMetricV3.CVSSV3.BaseScore
		published, err := time.Parse(publishedDateFmt, schema.PublishedDate)
		if err != nil {
			return fmt.Errorf("parse published_date: %w", err)
		}

		meta := fleet.CVEMeta{
			CVE:       cve,
			CVSSScore: &baseScore,
			Published: &published,
		}
		metaMap[cve] = meta
	}

	// load epss scores
	path := filepath.Join(vulnPath, strings.TrimSuffix(epssFilename, ".gz"))

	epssScores, err := parseEPSSScoresFile(path)
	if err != nil {
		return fmt.Errorf("parse epss scores: %w", err)
	}

	for _, epssScore := range epssScores {
		score, ok := metaMap[epssScore.CVE]
		if !ok {
			score.CVE = epssScore.CVE
		}
		score.EPSSProbability = &epssScore.Score
		metaMap[epssScore.CVE] = score
	}

	// load known exploits
	path = filepath.Join(vulnPath, cisaKnownExploitsFilename)
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	var catalog knownExploitedVulnerabilitiesCatalog
	if err := json.Unmarshal(b, &catalog); err != nil {
		return fmt.Errorf("unmarshal cisa known exploited vulnerabilities catalog: %w", err)
	}

	for _, vuln := range catalog.Vulnerabilities {
		score, ok := metaMap[vuln.CVEID]
		if !ok {
			score.CVE = vuln.CVEID
		}
		score.CISAKnownExploit = ptr.Bool(true)
		metaMap[vuln.CVEID] = score
	}

	// The catalog only contains "known" exploits, meaning all other CVEs should have known exploit set to false.
	for cve, meta := range metaMap {
		if meta.CISAKnownExploit == nil {
			meta.CISAKnownExploit = ptr.Bool(false)
		}
		metaMap[cve] = meta
	}

	if len(metaMap) == 0 {
		return nil
	}

	// convert to slice
	var meta []fleet.CVEMeta
	for _, score := range metaMap {
		meta = append(meta, score)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancel()

	if err := ds.InsertCVEMeta(ctx, meta); err != nil {
		return fmt.Errorf("insert cve meta: %w", err)
	}

	return nil
}