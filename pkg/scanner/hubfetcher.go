package scanner

import (
	"bitbucket.org/bdsengineering/go-hub-client/hubapi"
	"bitbucket.org/bdsengineering/go-hub-client/hubclient"
	log "github.com/sirupsen/logrus"
)

type HubFetcher struct {
	client     hubclient.Client
	username   string
	password   string
	baseURL    string
	isLoggedIn bool
}

func (hf *HubFetcher) login() error {
	if hf.isLoggedIn {
		return nil
	}
	// TODO figure out if the client stays logged in indefinitely,
	//   or if maybe it will need to be relogged in at some point.
	// For now, just assume it *will* stay logged in indefinitely.
	err := hf.client.Login(hf.username, hf.password)
	hf.isLoggedIn = (err == nil)
	return err
}

// NewHubFetcher returns a new, logged-in HubFetcher.
// It will instead return an error if either of the following happen:
//  - unable to instantiate a Hub API client
//  - unable to sign in to the Hub
func NewHubFetcher(username string, password string, baseURL string) (*HubFetcher, error) {
	client, err := hubclient.NewWithSession(baseURL, hubclient.HubClientDebugTimings)
	if err != nil {
		return nil, err
	}
	hf := HubFetcher{
		client:     *client,
		username:   username,
		password:   password,
		baseURL:    baseURL,
		isLoggedIn: false}
	err = hf.login()
	if err != nil {
		return nil, err
	}
	return &hf, nil
}

func (hf *HubFetcher) fetchProject(p hubapi.Project) (*Project, error) {
	client := hf.client
	project := Project{Name: p.Name, Source: p.Source, Versions: []Version{}}

	link, err := p.GetProjectVersionsLink()
	if err != nil {
		log.Errorf("error getting project versions link: %v", err)
		return nil, err
	}
	versions, err := client.ListProjectVersions(*link)
	if err != nil {
		log.Errorf("error fetching project version: %v", err)
		return nil, err
	}

	for _, v := range versions.Items {
		version := Version{
			Distribution:    v.Distribution,
			Nickname:        v.Nickname,
			Phase:           v.Phase,
			ReleaseComments: v.ReleaseComments,
			ReleasedOn:      v.ReleasedOn,
			VersionName:     v.VersionName,
			CodeLocations:   []CodeLocation{},
		}

		codeLocationsLink, err := v.GetCodeLocationsLink()
		if err != nil {
			log.Errorf("error getting code locations link: %v", err)
			return nil, err
		}
		codeLocations, err := client.ListCodeLocations(*codeLocationsLink)
		if err != nil {
			log.Errorf("error fetching code locations: %v", err)
			return nil, err
		}
		for _, cl := range codeLocations.Items {
			var codeLocation = CodeLocation{}
			codeLocation.CodeLocationType = cl.Type
			codeLocation.CreatedAt = cl.CreatedAt
			codeLocation.MappedProjectVersion = cl.MappedProjectVersion
			codeLocation.Name = cl.Name
			codeLocation.UpdatedAt = cl.UpdatedAt
			codeLocation.Url = cl.URL
			codeLocation.ScanSummaries = []ScanSummary{}

			scanSummariesLink, err := cl.GetScanSummariesLink()
			if err != nil {
				log.Errorf("error getting scan summaries link: %v", err)
				return nil, err
			}
			scanSummaries, err := client.ListScanSummaries(*scanSummariesLink)
			if err != nil {
				log.Errorf("error fetching scan summaries: %v", err)
				return nil, err
			}
			for _, scanSumy := range scanSummaries.Items {
				var scanSummary = ScanSummary{}
				scanSummary.CreatedAt = scanSumy.CreatedAt
				scanSummary.Status = scanSumy.Status
				scanSummary.UpdatedAt = scanSumy.UpdatedAt
				codeLocation.ScanSummaries = append(codeLocation.ScanSummaries, scanSummary)
			}

			version.CodeLocations = append(version.CodeLocations, codeLocation)
		}

		var riskProfile = RiskProfile{}
		riskProfileLink, err := v.GetProjectVersionRiskProfileLink()
		if err != nil {
			log.Errorf("error getting risk profile link: %v", err)
			return nil, err
		}
		rp, err := client.GetProjectVersionRiskProfile(*riskProfileLink)
		if err != nil {
			log.Errorf("error fetching project version risk profile: %v", err)
			return nil, err
		}
		riskProfile.BomLastUpdatedAt = rp.BomLastUpdatedAt
		riskProfile.Categories = rp.Categories
		version.RiskProfile = riskProfile

		policyStatusLink, err := v.GetProjectVersionPolicyStatusLink()
		if err != nil {
			log.Errorf("error getting policy status link: %v", err)
			return nil, err
		}
		ps, err := client.GetProjectVersionPolicyStatus(*policyStatusLink)
		if err != nil {
			log.Errorf("error fetching project version policy status: %v", err)
			return nil, err
		}
		statusCounts := make(map[string]int)
		for _, item := range ps.ComponentVersionStatusCounts {
			statusCounts[item.Name] = item.Value
		}
		version.PolicyStatus = PolicyStatus{
			OverallStatus:                ps.OverallStatus,
			ComponentVersionStatusCounts: statusCounts,
			UpdatedAt:                    ps.UpdatedAt,
		}

		project.Versions = append(project.Versions, version)
	}

	return &project, nil
}

// FetchProjectOfName searches for a project with the matching name,
//   returning a populated Project model
func (hf *HubFetcher) FetchProjectOfName(projectName string) (*Project, error) {
	// TODO instead of listing projects like this, do:
	//   https://34.227.56.110.xip.io/api/projects?&q=name:Perceptor
	// this will require a change in the go-hub-client library
	projs, err := hf.client.ListProjects()
	if err != nil {
		log.Errorf("error fetching project list: %v", err)
		return nil, err
	}
	for _, p := range projs.Items {
		if p.Name != projectName {
			// log.Info("skipping project ", p.Name, " as it doesn't match requested name ", projectName)
			continue
		}
		return hf.fetchProject(p)
	}
	return nil, nil
}