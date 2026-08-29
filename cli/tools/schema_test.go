package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	"github.com/yinhm/friendfeed/model"
	"github.com/yinhm/friendfeed/pb"
	"github.com/yinhm/friendfeed/store"
	"google.golang.org/protobuf/proto"
)

func TestVerifySchemaAcceptsCanonicalEmptyDatabase(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	result, err := verifySchema(db)
	require.NoError(t, err)
	require.Empty(t, result.Blockers)
	require.Equal(t, model.DBSchemaMissing, result.Schema.Status)
	require.NotEmpty(t, result.PebbleFormat)

	var out bytes.Buffer
	writeSchemaVerification(&out, result)
	require.Contains(t, out.String(), "application_schema=missing")
	require.Contains(t, out.String(), "ready=true")
}

func TestInspectSchemaOnlyReadsMetadata(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	key := model.NewUUIDKey(model.TableMeta, uuid.NewV5(uuid.NamespaceURL, "index:public:cache"))
	require.NoError(t, db.Set(key, []byte("legacy")))

	result, err := inspectSchema(db)
	require.NoError(t, err)
	require.Empty(t, result.Blockers)
	var out bytes.Buffer
	writeSchemaInspection(&out, result)
	require.Contains(t, out.String(), "application_schema=missing")
	require.NotContains(t, out.String(), "ready=")
	require.NotContains(t, out.String(), "blocker=")
	require.Contains(t, out.String(), "guidance=run verify_schema")
}

func TestVerifySchemaReportsRetiredPublicCache(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	key := model.NewUUIDKey(model.TableMeta, uuid.NewV5(uuid.NamespaceURL, "index:public:cache"))
	require.NoError(t, db.Set(key, []byte("legacy")))
	result, err := verifySchema(db)
	require.NoError(t, err)
	require.Equal(t, 1, blockerCount(result, "retired_public_cache"))
}

func TestVerifySchemaReportsLegacyDefaultPicture(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	profileID := uuid.Must(uuid.NewV4())
	_, err = model.Profile.Put(db, profileID.Bytes(), &pb.Profile{
		Uuid: profileID.String(), Id: "legacy-picture", Name: "Legacy", Type: "user",
		Picture: "https://friendfeed.com/static/images/group-large.png",
	})
	require.NoError(t, err)
	result, err := verifySchema(db)
	require.NoError(t, err)
	require.Equal(t, 1, blockerCount(result, "legacy_default_pictures"))
}

func TestVerifySchemaCombinesLegacyEntryChecks(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()

	entryID := uuid.Must(uuid.NewV4())
	entry := &pb.Entry{
		Id: entryID.String(), ProfileUuid: uuid.Must(uuid.NewV4()).String(),
		Date: "2026-01-01T00:00:00Z", Body: `<img src="http://i.friendfeed.com/legacy.jpg">`,
		Likes: []*pb.Like{{}},
	}
	raw, err := proto.Marshal(entry)
	require.NoError(t, err)
	require.NoError(t, db.Set(model.Entry.PrefixAppend(entryID.Bytes()), raw))

	legacy, err := scanSchemaLegacyRows(db)
	require.NoError(t, err)
	require.Equal(t, 1, legacy.embeddedInteractionEntries)
	require.Equal(t, 1, legacy.legacyMediaEntries)
}

func TestConfirmSchemaStamp(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, confirmSchemaStamp("/db", strings.NewReader("stamp_schema\n"), &out))
	require.Contains(t, out.String(), "certifies")
	require.Error(t, confirmSchemaStamp("/db", strings.NewReader("yes\n"), &out))
}

func TestStampSchemaDryRunAndPersistentApply(t *testing.T) {
	path := t.TempDir()
	db, err := store.NewStore(path)
	require.NoError(t, err)

	_, wrote, err := stampSchema(db, true)
	require.NoError(t, err)
	require.False(t, wrote)
	info, err := model.InspectDBSchema(db)
	require.NoError(t, err)
	require.Equal(t, model.DBSchemaMissing, info.Status)

	_, wrote, err = stampSchema(db, false)
	require.NoError(t, err)
	require.True(t, wrote)
	require.NoError(t, db.CloseWithError())

	db, err = store.NewStore(path)
	require.NoError(t, err)
	defer db.Close()
	info, err = model.InspectDBSchema(db)
	require.NoError(t, err)
	require.Equal(t, model.DBSchemaCurrent, info.Status)
	_, wrote, err = stampSchema(db, false)
	require.NoError(t, err)
	require.False(t, wrote)
}

func TestStampSchemaRefusesBlockers(t *testing.T) {
	db, err := store.NewStore(t.TempDir())
	require.NoError(t, err)
	defer db.Close()
	key := model.NewUUIDKey(model.TableMeta, uuid.NewV5(uuid.NamespaceURL, "index:public:cache"))
	require.NoError(t, db.Set(key, []byte("legacy")))

	_, wrote, err := stampSchema(db, false)
	require.ErrorContains(t, err, "blocker classes")
	require.False(t, wrote)
	info, inspectErr := model.InspectDBSchema(db)
	require.NoError(t, inspectErr)
	require.Equal(t, model.DBSchemaMissing, info.Status)
}

func TestSchemaVerificationExplainsBlockersAndWarnings(t *testing.T) {
	result := schemaVerification{
		Schema:       model.DBSchemaInfo{Status: model.DBSchemaMissing},
		PebbleFormat: "016",
		Blockers:     []schemaBlocker{{Name: "legacy_group_entry_authors", Count: 2}},
		Warnings:     []schemaBlocker{{Name: "groups_without_admins", Count: 262}},
	}
	var out bytes.Buffer
	writeSchemaVerification(&out, result)
	require.Contains(t, out.String(), "ready=false blockers=1 warnings=1")
	require.Contains(t, out.String(), "action=run migrate_group_entry_authors -dry-run")
	require.Contains(t, out.String(), "warning=groups_without_admins count=262")
	require.Contains(t, out.String(), "warnings may be handled separately")
}

func blockerCount(result schemaVerification, name string) int {
	for _, blocker := range result.Blockers {
		if blocker.Name == name {
			return blocker.Count
		}
	}
	return 0
}
