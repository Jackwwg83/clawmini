package server

import "database/sql"

type BatchJob struct {
	ID           string         `json:"id"`
	Command      string         `json:"command"`
	Status       string         `json:"status"`
	CreatedAt    int64          `json:"createdAt"`
	UpdatedAt    int64          `json:"updatedAt"`
	TotalCount   int            `json:"totalCount"`
	PendingCount int            `json:"pendingCount"`
	RunningCount int            `json:"runningCount"`
	SuccessCount int            `json:"successCount"`
	ErrorCount   int            `json:"errorCount"`
	Items        []BatchJobItem `json:"items"`
}

type BatchJobItem struct {
	DeviceID  string `json:"deviceId"`
	Status    string `json:"status"`
	CommandID string `json:"commandId,omitempty"`
	Error     string `json:"error,omitempty"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

type BatchJobStore struct {
	db *sql.DB
}

func NewBatchJobStore(db *sql.DB) *BatchJobStore {
	return &BatchJobStore{db: db}
}

func (s *BatchJobStore) EnsureSchema() error {
	return ensureSchemaMigrations(s.db, schemaNameBatchJobs, 1, map[int]string{
		1: `
CREATE TABLE IF NOT EXISTS batch_jobs (
	id TEXT PRIMARY KEY,
	command TEXT NOT NULL,
	status TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS batch_job_items (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	job_id TEXT NOT NULL REFERENCES batch_jobs(id) ON DELETE CASCADE,
	device_id TEXT NOT NULL,
	status TEXT NOT NULL,
	command_id TEXT,
	error TEXT,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL,
	UNIQUE(job_id, device_id)
);
CREATE INDEX IF NOT EXISTS idx_batch_job_items_job_id ON batch_job_items(job_id);
`,
	})
}

func (s *BatchJobStore) Create(command string, deviceIDs []string) (BatchJob, error) {
	now := nowUnix()
	id, err := randomHex(12)
	if err != nil {
		return BatchJob{}, err
	}

	tx, err := s.db.Begin()
	if err != nil {
		return BatchJob{}, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`
INSERT INTO batch_jobs(id, command, status, created_at, updated_at)
VALUES(?, ?, 'queued', ?, ?);
`, id, command, now, now); err != nil {
		return BatchJob{}, err
	}

	for _, deviceID := range deviceIDs {
		if _, err := tx.Exec(`
INSERT INTO batch_job_items(job_id, device_id, status, created_at, updated_at)
VALUES(?, ?, 'pending', ?, ?);
`, id, deviceID, now, now); err != nil {
			return BatchJob{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return BatchJob{}, err
	}
	return s.Get(id)
}

func (s *BatchJobStore) SetStatus(jobID, status string) error {
	_, err := s.db.Exec(`UPDATE batch_jobs SET status=?, updated_at=? WHERE id=?;`, status, nowUnix(), jobID)
	return err
}

func (s *BatchJobStore) UpdateItem(jobID, deviceID, status, commandID, errorText string) error {
	_, err := s.db.Exec(`
UPDATE batch_job_items
SET status=?, command_id=?, error=?, updated_at=?
WHERE job_id=? AND device_id=?;
`, status, commandID, errorText, nowUnix(), jobID, deviceID)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE batch_jobs SET updated_at=? WHERE id=?;`, nowUnix(), jobID)
	return err
}

func (s *BatchJobStore) Get(jobID string) (BatchJob, error) {
	var job BatchJob
	if err := s.db.QueryRow(`
SELECT id, command, status, created_at, updated_at
FROM batch_jobs
WHERE id=?;
`, jobID).Scan(&job.ID, &job.Command, &job.Status, &job.CreatedAt, &job.UpdatedAt); err != nil {
		if err == sql.ErrNoRows {
			return BatchJob{}, ErrNotFound
		}
		return BatchJob{}, err
	}

	rows, err := s.db.Query(`
SELECT device_id, status, command_id, error, created_at, updated_at
FROM batch_job_items
WHERE job_id=?
ORDER BY id ASC;
`, jobID)
	if err != nil {
		return BatchJob{}, err
	}
	defer rows.Close()

	items := make([]BatchJobItem, 0)
	for rows.Next() {
		var item BatchJobItem
		var commandID sql.NullString
		var errorText sql.NullString
		if err := rows.Scan(&item.DeviceID, &item.Status, &commandID, &errorText, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return BatchJob{}, err
		}
		if commandID.Valid {
			item.CommandID = commandID.String
		}
		if errorText.Valid {
			item.Error = errorText.String
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return BatchJob{}, err
	}

	job.Items = items
	job.TotalCount = len(items)
	for _, item := range items {
		switch item.Status {
		case "pending":
			job.PendingCount++
		case "running":
			job.RunningCount++
		case "success":
			job.SuccessCount++
		case "error":
			job.ErrorCount++
		}
	}
	return job, nil
}
