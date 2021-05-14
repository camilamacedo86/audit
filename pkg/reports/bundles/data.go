// Copyright 2021 The Audit Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package bundles

import (
	"fmt"
	"sort"

	log "github.com/sirupsen/logrus"

	sq "github.com/Masterminds/squirrel"
	"github.com/blang/semver"
	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/operator-framework/audit/pkg"

	"github.com/operator-framework/audit/pkg/models"
)

type Data struct {
	AuditBundle       []models.AuditBundle
	Flags             BindFlags
	IndexImageInspect pkg.DockerInspectManifest
}

func (d *Data) PrepareReport() Report {
	var allColumns []Columns
	for _, v := range d.AuditBundle {

		col := Columns{}

		// do not add bundle which has not the label
		if len(d.Flags.Label) > 0 && !v.FoundLabel {
			continue
		}

		col.InvalidSkipRange = pkg.NotUsed
		col.InvalidVersioning = pkg.Unknown
		col.PackageName = v.PackageName
		col.BundlePath = v.OperatorBundleImagePath
		col.OperatorBundleName = v.OperatorBundleName
		col.DefaultChannel = v.DefaultChannel
		col.Channels = v.Channels
		col.AuditErrors = v.Errors
		col.SkipRange = v.SkipRangeDB
		col.Replace = v.ReplacesDB
		col.OperatorBundleVersion = v.VersionDB
		col.OCPLabel = v.OCPLabel
		col.BuildAt = v.BuildAt

		var csv *v1alpha1.ClusterServiceVersion
		if v.Bundle != nil && v.Bundle.CSV != nil {
			csv = v.Bundle.CSV
		} else if v.CSVFromIndexDB != nil {
			csv = v.CSVFromIndexDB
		}

		col.AddDataFromCSV(csv)
		col.AddDataFromBundle(v.Bundle)
		col.AddDataFromScorecard(v.ScorecardResults)
		col.AddDataFromValidators(v.ValidatorsResults)

		if len(col.OperatorBundleVersion) < 1 && len(v.VersionDB) > 0 {
			col.OperatorBundleVersion = v.VersionDB
		}

		if len(col.OperatorBundleVersion) > 0 {
			_, err := semver.Parse(col.OperatorBundleVersion)
			if err != nil {
				col.InvalidVersioning = pkg.GetYesOrNo(true)
			} else {
				col.InvalidVersioning = pkg.GetYesOrNo(false)
			}
		}

		if len(col.SkipRange) > 0 {
			_, err := semver.ParseRange(col.SkipRange)
			if err != nil {
				col.InvalidSkipRange = pkg.GetYesOrNo(true)
			} else {
				col.InvalidSkipRange = pkg.GetYesOrNo(false)
			}
		}

		// Ignore this check if the head-only flag was used
		if !d.Flags.HeadOnly {
			if len(col.Replace) > 0 {
				// check if found replace
				col.FoundReplace = pkg.GetYesOrNo(false)
				for _, b := range d.AuditBundle {
					if b.OperatorBundleName == col.Replace {
						col.FoundReplace = pkg.GetYesOrNo(true)
						break
					}
				}
			}
		}

		allColumns = append(allColumns, col)
	}

	sort.Slice(allColumns[:], func(i, j int) bool {
		return allColumns[i].PackageName < allColumns[j].PackageName
	})

	finalReport := Report{}
	finalReport.Flags = d.Flags
	finalReport.Columns = allColumns
	finalReport.IndexImageInspect = d.IndexImageInspect

	if len(allColumns) == 0 {
		log.Fatal("No data was found for the criteria informed. " +
			"Please, ensure that you provide valid information.")
	}

	return finalReport
}

func (d *Data) OutputReport() error {

	report := d.PrepareReport()

	switch d.Flags.OutputFormat {
	case pkg.Xls:
		if err := report.writeXls(); err != nil {
			return err
		}
	case pkg.JSON:
		if err := report.writeJSON(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("invalid output format : %s", d.Flags.OutputFormat)
	}
	return nil
}

func (d *Data) BuildBundlesQuery() (string, error) {
	query := sq.Select("o.name, o.csv, o.bundlepath, o.version, o.skiprange, o.replaces, o.skips").From(
		"operatorbundle o")

	if d.Flags.HeadOnly {
		query = sq.Select("o.name, o.csv, o.bundlepath, o.version, o.skiprange, o.replaces, o.skips").From(
			"operatorbundle o, channel c")
		query = query.Where("c.head_operatorbundle_name == o.name")
	}
	if d.Flags.Limit > 0 {
		query = query.Limit(uint64(d.Flags.Limit))
	}
	if len(d.Flags.Filter) > 0 {
		query = sq.Select("o.name, o.csv, o.bundlepath, o.version, o.skiprange, o.replaces, o.skips").From(
			"operatorbundle o, channel_entry c")
		like := "'%" + d.Flags.Filter + "%'"
		query = query.Where(fmt.Sprintf("c.operatorbundle_name == o.name AND c.package_name like %s", like))
	}

	query.OrderBy("o.name")

	sql, _, err := query.ToSql()
	if err != nil {
		return "", fmt.Errorf("unable to create sql : %s", err)
	}
	return sql, nil
}
