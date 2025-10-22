package sqlite3

import (
    "context"
    "database/sql"
    "time"

    "github.com/element-hq/dendrite/internal/sqlutil"
    "github.com/element-hq/dendrite/userapi/storage/tables"
)

const redactionJobsSchema = `
CREATE TABLE IF NOT EXISTS userapi_redaction_jobs (
    job_id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT NOT NULL,
    requested_by TEXT NOT NULL,
    requested_ts BIGINT NOT NULL,
    status TEXT NOT NULL,
    redact_messages BOOLEAN NOT NULL
);
CREATE INDEX IF NOT EXISTS userapi_redaction_jobs_user_idx ON userapi_redaction_jobs(user_id);
`

const insertRedactionJobSQL = "INSERT INTO userapi_redaction_jobs (user_id, requested_by, requested_ts, status, redact_messages) VALUES (?, ?, ?, ?, ?)"
const selectRedactionJobsByUserSQL = "SELECT job_id, user_id, requested_by, requested_ts, status, redact_messages FROM userapi_redaction_jobs WHERE user_id = ? ORDER BY requested_ts DESC, job_id DESC"

type sqliteRedactionJobsTable struct {
    insertStmt *sql.Stmt
    selectStmt *sql.Stmt
}

func NewSQLiteUserRedactionJobsTable(db *sql.DB) (tables.UserRedactionJobsTable, error) {
    if _, err := db.Exec(redactionJobsSchema); err != nil {
        return nil, err
    }
    insertStmt, err := db.Prepare(insertRedactionJobSQL)
    if err != nil {
        return nil, err
    }
    selectStmt, err := db.Prepare(selectRedactionJobsByUserSQL)
    if err != nil {
        return nil, err
    }
    return &sqliteRedactionJobsTable{
        insertStmt: insertStmt,
        selectStmt: selectStmt,
    }, nil
}

func (s *sqliteRedactionJobsTable) InsertUserRedactionJob(ctx context.Context, txn *sql.Tx, job tables.UserRedactionJob) (int64, error) {
    stmt := sqlutil.TxStmt(txn, s.insertStmt)
    ts := job.RequestedTS.UTC().UnixMilli()
    res, err := stmt.ExecContext(ctx, job.UserID, job.RequestedBy, ts, job.Status, job.RedactMessages)
    if err != nil {
        return 0, err
    }
    id, err := res.LastInsertId()
    if err != nil {
        return 0, err
    }
    return id, nil
}

func (s *sqliteRedactionJobsTable) SelectUserRedactionJobsByUser(ctx context.Context, txn *sql.Tx, userID string) ([]tables.UserRedactionJob, error) {
    stmt := sqlutil.TxStmt(txn, s.selectStmt)
    rows, err := stmt.QueryContext(ctx, userID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var result []tables.UserRedactionJob
    for rows.Next() {
        var (
            job tables.UserRedactionJob
            ts  int64
        )
        if err := rows.Scan(&job.JobID, &job.UserID, &job.RequestedBy, &ts, &job.Status, &job.RedactMessages); err != nil {
            return nil, err
        }
        job.RequestedTS = time.UnixMilli(ts).UTC()
        result = append(result, job)
    }
    if err := rows.Err(); err != nil {
        return nil, err
    }
    return result, nil
}
