/*
Copyright (C) 2018 Black Duck Software, Inc.

Licensed to the Apache Software Foundation (ASF) under one
or more contributor license agreements. See the NOTICE file
distributed with this work for additional information
regarding copyright ownership. The ASF licenses this file
to you under the Apache License, Version 2.0 (the
"License"); you may not use this file except in compliance
with the License. You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing,
software distributed under the License is distributed on an
"AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
KIND, either express or implied. See the License for the
specific language governing permissions and limitations
under the License.
*/

package hub

type Version struct {
	CodeLocations   []CodeLocation
	RiskProfile     RiskProfile
	PolicyStatus    PolicyStatus
	Distribution    string
	Nickname        string
	VersionName     string
	ReleasedOn      string
	ReleaseComments string
	Phase           string
}

func (version *Version) IsImageScanDone() bool {
	// if there's at least 1 code location
	if len(version.CodeLocations) == 0 {
		return false
	}

	// and for each code location:
	for _, codeLocation := range version.CodeLocations {
		// there's at least 1 scan summary
		if len(codeLocation.ScanSummaries) == 0 {
			return false
		}

		for _, scanSummary := range codeLocation.ScanSummaries {
			// and for each scan summary:
			switch scanSummary.Status {
			case "ERROR", "ERROR_BUILDING_BOM", "ERROR_MATCHING", "ERROR_SAVING_SCAN_DATA", "ERROR_SCANNING", "CANCELLED", "COMPLETE":
				continue
			default:
				return false
			}
		}
	}

	// log.Infof("found a project version that's done: %v", version)

	// then it's done
	return true
}
