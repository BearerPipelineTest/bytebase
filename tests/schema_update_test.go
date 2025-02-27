package tests

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/bytebase/bytebase/api"
	"github.com/bytebase/bytebase/plugin/db"
	"github.com/bytebase/bytebase/plugin/vcs"
	"github.com/bytebase/bytebase/plugin/vcs/github"
	"github.com/bytebase/bytebase/plugin/vcs/gitlab"
	resourcemysql "github.com/bytebase/bytebase/resources/mysql"
	"github.com/bytebase/bytebase/tests/fake"
)

func TestSchemaAndDataUpdate(t *testing.T) {
	t.Parallel()
	a := require.New(t)
	ctx := context.Background()
	ctl := &controller{}
	dataDir := t.TempDir()
	err := ctl.StartServer(ctx, dataDir, fake.NewGitLab, getTestPort(t.Name()))
	a.NoError(err)
	defer ctl.Close(ctx)
	err = ctl.Login()
	a.NoError(err)

	err = ctl.setLicense()
	a.NoError(err)

	// Create a project.
	project, err := ctl.createProject(api.ProjectCreate{
		Name: "Test Project",
		Key:  "TestSchemaUpdate",
	})
	a.NoError(err)

	// Provision an instance.
	instanceRootDir := t.TempDir()
	instanceName := "testInstance1"
	instanceDir, err := ctl.provisionSQLiteInstance(instanceRootDir, instanceName)
	a.NoError(err)

	environments, err := ctl.getEnvironments()
	a.NoError(err)
	prodEnvironment, err := findEnvironment(environments, "Prod")
	a.NoError(err)

	// Add an instance.
	instance, err := ctl.addInstance(api.InstanceCreate{
		EnvironmentID: prodEnvironment.ID,
		Name:          instanceName,
		Engine:        db.SQLite,
		Host:          instanceDir,
	})
	a.NoError(err)

	// Expecting project to have no database.
	databases, err := ctl.getDatabases(api.DatabaseFind{
		ProjectID: &project.ID,
	})
	a.NoError(err)
	a.Zero(len(databases))
	// Expecting instance to have no database.
	databases, err = ctl.getDatabases(api.DatabaseFind{
		InstanceID: &instance.ID,
	})
	a.NoError(err)
	a.Zero(len(databases))

	// Create an issue that creates a database.
	databaseName := "testSchemaUpdate"
	err = ctl.createDatabase(project, instance, databaseName, "", nil /* labelMap */)
	a.NoError(err)

	// Expecting project to have 1 database.
	databases, err = ctl.getDatabases(api.DatabaseFind{
		ProjectID: &project.ID,
	})
	a.NoError(err)
	a.Equal(1, len(databases))
	database := databases[0]
	a.Equal(instance.ID, database.Instance.ID)

	// Create an issue that updates database schema.
	createContext, err := json.Marshal(&api.UpdateSchemaContext{
		MigrationType: db.Migrate,
		DetailList: []*api.UpdateSchemaDetail{
			{
				DatabaseID: database.ID,
				Statement:  migrationStatement,
			},
		},
	})
	a.NoError(err)
	issue, err := ctl.createIssue(api.IssueCreate{
		ProjectID:   project.ID,
		Name:        fmt.Sprintf("update schema for database %q", databaseName),
		Type:        api.IssueDatabaseSchemaUpdate,
		Description: fmt.Sprintf("This updates the schema of database %q.", databaseName),
		// Assign to self.
		AssigneeID:    project.Creator.ID,
		CreateContext: string(createContext),
	})
	a.NoError(err)
	status, err := ctl.waitIssuePipeline(issue.ID)
	a.NoError(err)
	a.Equal(api.TaskDone, status)

	// Query schema.
	result, err := ctl.query(instance, databaseName, bookTableQuery)
	a.NoError(err)
	a.Equal(bookSchemaSQLResult, result)

	// Create an issue that updates database data.
	createContext, err = json.Marshal(&api.UpdateSchemaContext{
		MigrationType: db.Data,
		DetailList: []*api.UpdateSchemaDetail{
			{
				DatabaseID: database.ID,
				Statement:  dataUpdateStatement,
			},
		},
	})
	a.NoError(err)
	issue, err = ctl.createIssue(api.IssueCreate{
		ProjectID:   project.ID,
		Name:        fmt.Sprintf("update data for database %q", databaseName),
		Type:        api.IssueDatabaseDataUpdate,
		Description: fmt.Sprintf("This updates the data of database %q.", databaseName),
		// Assign to self.
		AssigneeID:    project.Creator.ID,
		CreateContext: string(createContext),
	})
	a.NoError(err)
	status, err = ctl.waitIssuePipeline(issue.ID)
	a.NoError(err)
	a.Equal(api.TaskDone, status)

	// Get migration history.
	histories, err := ctl.getInstanceMigrationHistory(db.MigrationHistoryFind{ID: &instance.ID})
	a.NoError(err)
	wantHistories := []api.MigrationHistory{
		{
			Database:   databaseName,
			Source:     db.UI,
			Type:       db.Data,
			Status:     db.Done,
			Schema:     dumpedSchema,
			SchemaPrev: dumpedSchema,
		},
		{
			Database:   databaseName,
			Source:     db.UI,
			Type:       db.Migrate,
			Status:     db.Done,
			Schema:     dumpedSchema,
			SchemaPrev: "",
		},
		{
			Database:   databaseName,
			Source:     db.UI,
			Type:       db.Migrate,
			Status:     db.Done,
			Schema:     "",
			SchemaPrev: "",
		},
	}
	a.Equal(len(histories), len(wantHistories))
	for i, history := range histories {
		got := api.MigrationHistory{
			Database:   history.Database,
			Source:     history.Source,
			Type:       history.Type,
			Status:     history.Status,
			Schema:     history.Schema,
			SchemaPrev: history.SchemaPrev,
		}
		want := wantHistories[i]
		a.Equal(want, got)
		a.NotEqual(history.Version, "")
	}

	// Create a manual backup.
	backup, err := ctl.createBackup(api.BackupCreate{
		DatabaseID:     database.ID,
		Name:           "name",
		Type:           api.BackupTypeManual,
		StorageBackend: api.BackupStorageBackendLocal,
	})
	a.NoError(err)
	err = ctl.waitBackup(backup.DatabaseID, backup.ID)
	a.NoError(err)

	backupPath := path.Join(dataDir, backup.Path)
	backupContent, err := os.ReadFile(backupPath)
	a.NoError(err)
	a.Equal(string(backupContent), backupDump)

	// Create an issue that creates a database.
	cloneDatabaseName := "testClone"
	err = ctl.cloneDatabaseFromBackup(project, instance, cloneDatabaseName, backup, nil /* labelMap */)
	a.NoError(err)

	// Query clone database book table data.
	result, err = ctl.query(instance, cloneDatabaseName, bookDataQuery)
	a.NoError(err)
	a.Equal(bookDataSQLResult, result)
	// Query clone migration history.
	histories, err = ctl.getInstanceMigrationHistory(db.MigrationHistoryFind{ID: &instance.ID, Database: &cloneDatabaseName})
	a.NoError(err)
	wantCloneHistories := []api.MigrationHistory{
		{
			Database:   cloneDatabaseName,
			Source:     db.UI,
			Type:       db.Branch,
			Status:     db.Done,
			Schema:     dumpedSchema,
			SchemaPrev: dumpedSchema,
		},
		{
			Database:   cloneDatabaseName,
			Source:     db.UI,
			Type:       db.Migrate,
			Status:     db.Done,
			Schema:     "",
			SchemaPrev: "",
		},
	}
	a.Equal(len(histories), len(wantCloneHistories))
	for i, history := range histories {
		got := api.MigrationHistory{
			Database:   history.Database,
			Source:     history.Source,
			Type:       history.Type,
			Status:     history.Status,
			Schema:     history.Schema,
			SchemaPrev: history.SchemaPrev,
		}
		want := wantCloneHistories[i]
		a.Equal(want, got)
	}

	// Create a sheet to mock SQL editor new tab action with UNKNOWN ProjectID.
	_, err = ctl.createSheet(api.SheetCreate{
		ProjectID:  -1,
		DatabaseID: &database.ID,
		Name:       "my-sheet",
		Statement:  "SELECT * FROM demo",
		Visibility: api.PrivateSheet,
	})
	a.NoError(err)

	_, err = ctl.listSheets(api.SheetFind{
		DatabaseID: &database.ID,
	})
	a.NoError(err)

	// Test if POST /api/database/:id/data-source api is working right.
	// TODO(steven): I will add read-only data source testing to a separate test later.
	err = ctl.createDataSource(api.DataSourceCreate{
		InstanceID: instance.ID,
		DatabaseID: database.ID,
		CreatorID:  project.Creator.ID,
		Name:       "ADMIN data source",
		Type:       "ADMIN",
		Username:   "root",
		Password:   "",
	})
	a.NoError(err)
}

func TestVCS(t *testing.T) {
	tests := []struct {
		name                string
		vcsProviderCreator  fake.VCSProviderCreator
		vcsType             vcs.Type
		externalID          string
		repositoryFullPath  string
		newWebhookPushEvent func(gitFile string) interface{}
	}{
		{
			name:               "GitLab",
			vcsProviderCreator: fake.NewGitLab,
			vcsType:            vcs.GitLabSelfHost,
			externalID:         "121",
			repositoryFullPath: "test/schemaUpdate",
			newWebhookPushEvent: func(gitFile string) interface{} {
				return gitlab.WebhookPushEvent{
					ObjectKind: gitlab.WebhookPush,
					Ref:        "refs/heads/feature/foo",
					Project: gitlab.WebhookProject{
						ID: 121,
					},
					CommitList: []gitlab.WebhookCommit{
						{
							Timestamp: "2021-01-13T13:14:00Z",
							AddedList: []string{gitFile},
						},
					},
				}
			},
		},
		{
			name:               "GitHub",
			vcsProviderCreator: fake.NewGitHub,
			vcsType:            vcs.GitHubCom,
			externalID:         "octocat/Hello-World",
			repositoryFullPath: "octocat/Hello-World",
			newWebhookPushEvent: func(gitFile string) interface{} {
				return github.WebhookPushEvent{
					Ref: "refs/heads/feature/foo",
					Repository: github.WebhookRepository{
						ID:       211,
						FullName: "octocat/Hello-World",
						HTMLURL:  "https://github.com/octocat/Hello-World",
					},
					Sender: github.WebhookSender{
						Login: "fake_github_author",
					},
					Commits: []github.WebhookCommit{
						{
							ID:        "fake_github_commit_id",
							Distinct:  true,
							Message:   "Fake GitHub commit message",
							Timestamp: time.Now(),
							URL:       "https://api.github.com/octocat/Hello-World/commits/fake_github_commit_id",
							Author: github.WebhookCommitAuthor{
								Name:  "fake_github_author",
								Email: "fake_github_author@localhost",
							},
							Added: []string{gitFile},
						},
					},
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			a := require.New(t)
			ctx := context.Background()
			ctl := &controller{}
			err := ctl.StartServer(ctx, t.TempDir(), test.vcsProviderCreator, getTestPort(t.Name()))
			a.NoError(err)
			defer func() {
				_ = ctl.Close(ctx)
			}()

			err = ctl.Login()
			a.NoError(err)
			err = ctl.setLicense()
			a.NoError(err)

			// Create a VCS.
			vcs, err := ctl.createVCS(
				api.VCSCreate{
					Name:          t.Name(),
					Type:          test.vcsType,
					InstanceURL:   ctl.vcsURL,
					APIURL:        ctl.vcsProvider.APIURL(ctl.vcsURL),
					ApplicationID: "testApplicationID",
					Secret:        "testApplicationSecret",
				},
			)
			a.NoError(err)

			// Create a project.
			project, err := ctl.createProject(
				api.ProjectCreate{
					Name: "Test VCS Project",
					Key:  "TestVCSSchemaUpdate",
				},
			)
			a.NoError(err)

			// Create a repository.
			ctl.vcsProvider.CreateRepository(test.externalID)
			_, err = ctl.createRepository(
				api.RepositoryCreate{
					VCSID:              vcs.ID,
					ProjectID:          project.ID,
					Name:               "Test Repository",
					FullPath:           test.repositoryFullPath,
					WebURL:             fmt.Sprintf("%s/%s", ctl.vcsURL, test.repositoryFullPath),
					BranchFilter:       "feature/foo",
					BaseDirectory:      baseDirectory,
					FilePathTemplate:   "{{ENV_NAME}}/{{DB_NAME}}__{{VERSION}}__{{TYPE}}__{{DESCRIPTION}}.sql",
					SchemaPathTemplate: "{{ENV_NAME}}/.{{DB_NAME}}__LATEST.sql",
					ExternalID:         test.externalID,
					AccessToken:        "accessToken1",
					RefreshToken:       "refreshToken1",
				},
			)
			a.NoError(err)

			// Provision an instance.
			instanceName := "testInstance1"
			instanceDir, err := ctl.provisionSQLiteInstance(t.TempDir(), instanceName)
			a.NoError(err)

			environments, err := ctl.getEnvironments()
			a.NoError(err)
			prodEnvironment, err := findEnvironment(environments, "Prod")
			a.NoError(err)

			// Add an instance.
			instance, err := ctl.addInstance(api.InstanceCreate{
				EnvironmentID: prodEnvironment.ID,
				Name:          instanceName,
				Engine:        db.SQLite,
				Host:          instanceDir,
			})
			a.NoError(err)

			// Create an issue that creates a database.
			databaseName := "testVCSSchemaUpdate"
			err = ctl.createDatabase(project, instance, databaseName, "", nil /* labelMap */)
			a.NoError(err)

			// Simulate Git commits for schema update.
			gitFile := "bbtest/Prod/testVCSSchemaUpdate__ver1__migrate__create_a_test_table.sql"
			err = ctl.vcsProvider.AddFiles(test.externalID, map[string]string{gitFile: migrationStatement})
			a.NoError(err)

			payload, err := json.Marshal(test.newWebhookPushEvent(gitFile))
			a.NoError(err)
			err = ctl.vcsProvider.SendWebhookPush(test.externalID, payload)
			a.NoError(err)

			// Get schema update issue.
			openStatus := []api.IssueStatus{api.IssueOpen}
			issues, err := ctl.getIssues(
				api.IssueFind{
					ProjectID:  &project.ID,
					StatusList: openStatus,
				},
			)
			a.NoError(err)
			a.Len(issues, 1)
			issue := issues[0]
			status, err := ctl.waitIssuePipeline(issue.ID)
			a.NoError(err)
			a.Equal(api.TaskDone, status)
			_, err = ctl.patchIssueStatus(
				api.IssueStatusPatch{
					ID:     issue.ID,
					Status: api.IssueDone,
				},
			)
			a.NoError(err)

			// Query schema.
			result, err := ctl.query(instance, databaseName, bookTableQuery)
			a.NoError(err)
			a.Equal(bookSchemaSQLResult, result)

			// Simulate Git commits for schema update.
			gitFile = "bbtest/Prod/testVCSSchemaUpdate__ver2__data__insert_data.sql"
			err = ctl.vcsProvider.AddFiles(test.externalID, map[string]string{gitFile: dataUpdateStatement})
			a.NoError(err)

			payload, err = json.Marshal(test.newWebhookPushEvent(gitFile))
			a.NoError(err)
			err = ctl.vcsProvider.SendWebhookPush(test.externalID, payload)
			a.NoError(err)

			// Get data update issue.
			openStatus = []api.IssueStatus{api.IssueOpen}
			issues, err = ctl.getIssues(
				api.IssueFind{
					ProjectID:  &project.ID,
					StatusList: openStatus,
				},
			)
			a.NoError(err)
			a.Len(issues, 1)
			issue = issues[0]
			status, err = ctl.waitIssuePipeline(issue.ID)
			a.NoError(err)
			a.Equal(api.TaskDone, status)
			_, err = ctl.patchIssueStatus(
				api.IssueStatusPatch{
					ID:     issue.ID,
					Status: api.IssueDone,
				},
			)
			a.NoError(err)

			// Get migration history.
			histories, err := ctl.getInstanceMigrationHistory(db.MigrationHistoryFind{ID: &instance.ID})
			a.NoError(err)
			wantHistories := []api.MigrationHistory{
				{
					Database:   databaseName,
					Source:     db.VCS,
					Type:       db.Data,
					Status:     db.Done,
					Schema:     dumpedSchema,
					SchemaPrev: dumpedSchema,
				},
				{
					Database:   databaseName,
					Source:     db.VCS,
					Type:       db.Migrate,
					Status:     db.Done,
					Schema:     dumpedSchema,
					SchemaPrev: "",
				},
				{
					Database:   databaseName,
					Source:     db.UI,
					Type:       db.Migrate,
					Status:     db.Done,
					Schema:     "",
					SchemaPrev: "",
				},
			}
			a.Equal(len(wantHistories), len(histories))

			for i, history := range histories {
				got := api.MigrationHistory{
					Database:   history.Database,
					Source:     history.Source,
					Type:       history.Type,
					Status:     history.Status,
					Schema:     history.Schema,
					SchemaPrev: history.SchemaPrev,
				}
				a.Equal(wantHistories[i], got)
				a.NotEmpty(history.Version)
			}
			a.Equal(histories[0].Version, "ver2")
			a.Equal(histories[1].Version, "ver1")
		})
	}
}

func TestWildcardInVCSFilePathTemplate(t *testing.T) {
	branchFilter := "feature/foo"
	dbName := "db"
	externalID := "121"
	repoFullPath := "test/wildcard"

	tests := []struct {
		name                string
		vcsProviderCreator  fake.VCSProviderCreator
		vcsType             vcs.Type
		baseDirectory       string
		envName             string
		filePathTemplate    string
		commitFileNames     []string
		commitContents      []string
		expect              []bool
		newWebhookPushEvent func(gitFile string) interface{}
	}{
		{
			name:               "singleAsterisk",
			vcsProviderCreator: fake.NewGitLab,
			vcsType:            vcs.GitLabSelfHost,
			baseDirectory:      "bbtest",
			envName:            "wildcard",
			filePathTemplate:   "{{ENV_NAME}}/*/{{DB_NAME}}__{{VERSION}}__{{TYPE}}__{{DESCRIPTION}}.sql",
			commitFileNames: []string{
				// Normal
				fmt.Sprintf("%s/%s/foo/%s__ver1__migrate__create_table_t1.sql", baseDirectory, "wildcard", dbName),
				// One singleAsterisk cannot match two directories.
				fmt.Sprintf("%s/%s/foo/bar/%s__ver2__data__insert_data.sql", baseDirectory, "wildcard", dbName),
				// One singleAsterisk cannot match zero directory.
				fmt.Sprintf("%s/%s/%s__ver3__migrate__create_table_t3.sql", baseDirectory, "wildcard", dbName),
			},
			commitContents: []string{
				"CREATE TABLE t1 (id INT);",
				"INSERT INTO t1 VALUES (1);",
				"CREATE TABLE t3 (id INT);",
			},
			expect: []bool{
				true,
				false,
				false,
			},
			newWebhookPushEvent: func(gitFile string) interface{} {
				return gitlab.WebhookPushEvent{
					ObjectKind: gitlab.WebhookPush,
					Ref:        fmt.Sprintf("refs/heads/%s", branchFilter),
					Project: gitlab.WebhookProject{
						ID: 121,
					},
					CommitList: []gitlab.WebhookCommit{
						{
							Timestamp: "2021-01-13T13:14:00Z",
							AddedList: []string{gitFile},
						},
					},
				}
			},
		},
		{
			name:               "doubleAsterisks",
			vcsProviderCreator: fake.NewGitLab,
			vcsType:            vcs.GitLabSelfHost,
			baseDirectory:      "bbtest",
			envName:            "wildcard",
			filePathTemplate:   "{{ENV_NAME}}/**/{{DB_NAME}}__{{VERSION}}__{{TYPE}}__{{DESCRIPTION}}.sql",
			commitFileNames: []string{
				// Two singleAsterisk can match one directory.
				fmt.Sprintf("%s/%s/foo/%s__ver1__migrate__create_table_t1.sql", baseDirectory, "wildcard", dbName),
				// Two singleAsterisk can match two directories.
				fmt.Sprintf("%s/%s/foo/bar/%s__ver2__migrate__create_table_t2.sql", baseDirectory, "wildcard", dbName),
				// Two singleAsterisk can match three directories or more.
				fmt.Sprintf("%s/%s/foo/bar/foo/%s__ver3__migrate__create_table_t3.sql", baseDirectory, "wildcard", dbName),
				// Two singleAsterisk cannot match zero directory.
				fmt.Sprintf("%s/%s/%s__ver4__migrate__create_table_t4.sql", baseDirectory, "wildcard", dbName),
			},
			commitContents: []string{
				"CREATE TABLE t1 (id INT);",
				"CREATE TABLE t2 (id INT);",
				"CREATE TABLE t3 (id INT);",
				"CREATE TABLE t4 (id INT);",
			},
			expect: []bool{
				true,
				true,
				true,
				false,
			},
			newWebhookPushEvent: func(gitFile string) interface{} {
				return gitlab.WebhookPushEvent{
					ObjectKind: gitlab.WebhookPush,
					Ref:        fmt.Sprintf("refs/heads/%s", branchFilter),
					Project: gitlab.WebhookProject{
						ID: 121,
					},
					CommitList: []gitlab.WebhookCommit{
						{
							Timestamp: "2021-01-13T13:14:00Z",
							AddedList: []string{gitFile},
						},
					},
				}
			},
		},
		{
			name:               "emptyBaseAndMixAsterisks",
			vcsProviderCreator: fake.NewGitLab,
			envName:            "wildcard",
			baseDirectory:      "",
			vcsType:            vcs.GitLabSelfHost,
			filePathTemplate:   "{{ENV_NAME}}/**/foo/*/{{DB_NAME}}__{{VERSION}}__{{TYPE}}__{{DESCRIPTION}}.sql",
			commitFileNames: []string{
				// ** matches foo, foo matches foo, * matches bar
				fmt.Sprintf("%s/foo/foo/bar/%s__ver1__migrate__create_table_t1.sql", "wildcard", dbName),
				// ** matches foo/bar/foo, foo matches foo, * matches bar
				fmt.Sprintf("%s/foo/bar/foo/foo/bar/%s__ver2__migrate__create_table_t2.sql", "wildcard", dbName),
				// cannot match
				fmt.Sprintf("%s/%s__ver3__migrate__create_table_t3.sql", "wildcard", dbName),
			},
			commitContents: []string{
				"CREATE TABLE t1 (id INT);",
				"CREATE TABLE t2 (id INT);",
				"CREATE TABLE t3 (id INT);",
			},
			expect: []bool{
				true,
				true,
				false,
			},
			newWebhookPushEvent: func(gitFile string) interface{} {
				return gitlab.WebhookPushEvent{
					ObjectKind: gitlab.WebhookPush,
					Ref:        fmt.Sprintf("refs/heads/%s", branchFilter),
					Project: gitlab.WebhookProject{
						ID: 121,
					},
					CommitList: []gitlab.WebhookCommit{
						{
							Timestamp: "2021-01-13T13:14:00Z",
							AddedList: []string{gitFile},
						},
					},
				}
			},
		},
		// We test the combination of ** and *, and the place holder is not fully represented by the ascii character set.
		{
			name:               "mixAsterisks",
			vcsProviderCreator: fake.NewGitLab,
			envName:            "生产",
			baseDirectory:      "bbtest",
			vcsType:            vcs.GitLabSelfHost,
			filePathTemplate:   "{{ENV_NAME}}/**/foo/*/{{DB_NAME}}__{{VERSION}}__{{TYPE}}__{{DESCRIPTION}}.sql",
			commitFileNames: []string{
				// ** matches foo, foo matches foo, * matches bar
				fmt.Sprintf("%s/%s/foo/foo/bar/%s__ver1__migrate__create_table_t1.sql", baseDirectory, "生产", dbName),
				// ** matches foo/bar/foo, foo matches foo, * matches bar
				fmt.Sprintf("%s/%s/foo/bar/foo/foo/bar/%s__ver2__migrate__create_table_t2.sql", baseDirectory, "生产", dbName),
				// cannot match
				fmt.Sprintf("%s/%s/%s__ver3__migrate__create_table_t3.sql", baseDirectory, "生产", dbName),
			},
			commitContents: []string{
				"CREATE TABLE t1 (id INT);",
				"CREATE TABLE t2 (id INT);",
				"CREATE TABLE t3 (id INT);",
			},
			expect: []bool{
				true,
				true,
				false,
			},
			newWebhookPushEvent: func(gitFile string) interface{} {
				return gitlab.WebhookPushEvent{
					ObjectKind: gitlab.WebhookPush,
					Ref:        fmt.Sprintf("refs/heads/%s", branchFilter),
					Project: gitlab.WebhookProject{
						ID: 121,
					},
					CommitList: []gitlab.WebhookCommit{
						{
							Timestamp: "2021-01-13T13:14:00Z",
							AddedList: []string{gitFile},
						},
					},
				}
			},
		},
		// No asterisk
		{
			name:               "placeholderAsFolder",
			vcsProviderCreator: fake.NewGitLab,
			envName:            "ZO",
			baseDirectory:      "bbtest",
			vcsType:            vcs.GitLabSelfHost,
			filePathTemplate:   "{{ENV_NAME}}/{{DB_NAME}}/sql/{{DB_NAME}}__{{VERSION}}__{{TYPE}}__{{DESCRIPTION}}.sql",
			commitFileNames: []string{
				fmt.Sprintf("%s/%s/%s/sql/%s__ver1__migrate__create_table_t1.sql", baseDirectory, "ZO", dbName, dbName),
				fmt.Sprintf("%s/%s/%s/%s__ver2__migrate__create_table_t2.sql", baseDirectory, "ZO", dbName, dbName),
				fmt.Sprintf("%s/%s/%s/sql/%s__ver3__migrate__create_table_t3.sql", baseDirectory, "ZO", dbName, dbName),
			},
			commitContents: []string{
				"CREATE TABLE t1 (id INT);",
				"CREATE TABLE t2 (id INT);",
				"CREATE TABLE t3 (id INT);",
			},
			expect: []bool{
				true,
				false,
				true,
			},
			newWebhookPushEvent: func(gitFile string) interface{} {
				return gitlab.WebhookPushEvent{
					ObjectKind: gitlab.WebhookPush,
					Ref:        fmt.Sprintf("refs/heads/%s", branchFilter),
					Project: gitlab.WebhookProject{
						ID: 121,
					},
					CommitList: []gitlab.WebhookCommit{
						{
							Timestamp: "2021-01-13T13:14:00Z",
							AddedList: []string{gitFile},
						},
					},
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			port := getTestPort(t.Name())
			mysqlPort := port + 3
			a := require.New(t)
			ctx := context.Background()
			ctl := &controller{}
			err := ctl.StartServer(ctx, t.TempDir(), test.vcsProviderCreator, port)
			a.NoError(err)
			defer func() {
				_ = ctl.Close(ctx)
			}()

			err = ctl.Login()
			a.NoError(err)
			err = ctl.setLicense()
			a.NoError(err)

			// Create a VCS.
			vcs, err := ctl.createVCS(
				api.VCSCreate{
					Name:          t.Name(),
					Type:          test.vcsType,
					InstanceURL:   ctl.vcsURL,
					APIURL:        ctl.vcsProvider.APIURL(ctl.vcsURL),
					ApplicationID: "testApplicationID",
					Secret:        "testApplicationSecret",
				},
			)
			a.NoError(err)

			// Create a project.
			project, err := ctl.createProject(
				api.ProjectCreate{
					Name: "Test VCS Project",
					Key:  "TVP",
				},
			)
			a.NoError(err)

			// Create a repository.
			ctl.vcsProvider.CreateRepository(externalID)
			_, err = ctl.createRepository(
				api.RepositoryCreate{
					VCSID:              vcs.ID,
					ProjectID:          project.ID,
					Name:               "Test Repository",
					FullPath:           repoFullPath,
					WebURL:             fmt.Sprintf("%s/%s", ctl.vcsURL, repoFullPath),
					BranchFilter:       branchFilter,
					BaseDirectory:      baseDirectory,
					FilePathTemplate:   test.filePathTemplate,
					SchemaPathTemplate: "{{ENV_NAME}}/.{{DB_NAME}}__LATEST.sql",
					ExternalID:         externalID,
					AccessToken:        "accessToken1",
					RefreshToken:       "refreshToken1",
				},
			)
			a.NoError(err)

			environment, err := ctl.createEnvrionment(api.EnvironmentCreate{
				Name: test.envName,
			})
			a.NoError(err)
			// Provision an instance.
			instanceName := "testInstance"
			_, stopInstance := resourcemysql.SetupTestInstance(t, mysqlPort)
			defer stopInstance()
			connCfg := getMySQLConnectionConfig(strconv.Itoa(mysqlPort), "")
			instance, err := ctl.addInstance(api.InstanceCreate{
				EnvironmentID: environment.ID,
				Name:          instanceName,
				Engine:        db.MySQL,
				Host:          connCfg.Host,
				Port:          connCfg.Port,
				Username:      connCfg.Username,
			})
			a.NoError(err)

			// Create an issue that creates a database.
			err = ctl.createDatabase(project, instance, dbName, "", nil /* labelMap */)
			a.NoError(err)

			a.Equal(len(test.expect), len(test.commitFileNames))
			a.Equal(len(test.expect), len(test.commitContents))
			for idx, commitFileName := range test.commitFileNames {
				// Simulate Git commits for schema update.
				err = ctl.vcsProvider.AddFiles(externalID, map[string]string{commitFileName: test.commitContents[idx]})
				a.NoError(err)

				payload, err := json.Marshal(test.newWebhookPushEvent(commitFileName))
				a.NoError(err)
				err = ctl.vcsProvider.SendWebhookPush(externalID, payload)
				a.NoError(err)

				// Check for newly generated issues.
				openStatus := []api.IssueStatus{api.IssueOpen}
				issues, err := ctl.getIssues(
					api.IssueFind{
						ProjectID:  &project.ID,
						StatusList: openStatus,
					},
				)
				a.NoError(err)
				if test.expect[idx] {
					a.Len(issues, 1)
					issue := issues[0]
					status, err := ctl.waitIssuePipeline(issue.ID)
					a.NoError(err)
					a.Equal(api.TaskDone, status)
					_, err = ctl.patchIssueStatus(
						api.IssueStatusPatch{
							ID:     issue.ID,
							Status: api.IssueDone,
						},
					)
					a.NoError(err)
				} else {
					a.Len(issues, 0)
				}
			}
		})
	}
}
