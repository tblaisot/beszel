package hub

import (
	"strconv"
	"strings"

	"github.com/henrygd/beszel/internal/hub/utils"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
)

// accessibleSystemIDs returns the systems the requesting user may read, mirroring the
// systems list rule (members only, or everyone when SHARE_ALL_SYSTEMS is set).
func accessibleSystemIDs(e *core.RequestEvent) ([]string, error) {
	shareAll, _ := utils.GetEnv("SHARE_ALL_SYSTEMS")
	var records []*core.Record
	var err error
	if shareAll == "true" || e.Auth.Collection().Name == core.CollectionNameSuperusers {
		records, err = e.App.FindAllRecords("systems")
	} else {
		records, err = e.App.FindRecordsByFilter("systems", "users.id ?= {:uid}", "", 0, 0, dbx.Params{"uid": e.Auth.Id})
	}
	if err != nil {
		return nil, err
	}
	ids := make([]string, len(records))
	for i, record := range records {
		ids[i] = record.Id
	}
	return ids, nil
}

// inClause returns the placeholders for an SQL IN list and adds their values to params.
// prefix keeps the parameter names unique when a query holds several lists.
func inClause(prefix string, values []string, params dbx.Params) string {
	placeholders := make([]string, len(values))
	for i, value := range values {
		name := prefix + strconv.Itoa(i)
		placeholders[i] = "{:" + name + "}"
		params[name] = value
	}
	return strings.Join(placeholders, ",")
}
