package server

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"sync"
	"time"
)

const (
	schemaNameIMConfigJobs = "im_config_jobs"

	defaultIMJobGCInterval = time.Hour
	defaultIMJobRetention  = 24 * time.Hour
)

// IMConfigJob mirrors the API response shape for configure-im jobs.
type IMConfigJob struct {
	ID        string         `json:"id"`
	DeviceID  string         `json:"deviceId"`
	Platform  string         `json:"platform"`
	Plugin    string         `json:"plugin,omitempty"`
	Status    string         `json:"status"`
	Error     string         `json:"error,omitempty"`
	Steps     []IMConfigStep `json:"steps"`
	CreatedAt int64          `json:"createdAt"`
	UpdatedAt int64          `json:"updatedAt"`
}

type IMConfigStep struct {
	Key            string         `json:"key"`
	Title          string         `json:"title"`
	DisplayCommand string         `json:"displayCommand"`
	Status         string         `json:"status"`
	CommandID      string         `json:"commandId,omitempty"`
	Error          string         `json:"error,omitempty"`
	Record         *CommandRecord `json:"record,omitempty"`
}

// IMConfigJobStore stores IM configuration jobs in SQLite and runs periodic GC.
type IMConfigJobStore struct {
	db *sql.DB

	gcInterval time.Duration
	retention  time.Duration

	updateMu sync.Mutex

	lifecycleMu sync.Mutex
	running     bool
	stopCh      chan struct{}
	doneCh      chan struct{}
}

func NewIMConfigJobStore(db *sql.DB) *IMConfigJobStore {
	return newIMConfigJobStoreWithIntervals(db, defaultIMJobGCInterval, defaultIMJobRetention)
}

func newIMConfigJobStoreWithIntervals(db *sql.DB, gcInterval, retention time.Duration) *IMConfigJobStore {
	if gcInterval <= 0 {
		gcInterval = defaultIMJobGCInterval
	}
	if retention <= 0 {
		retention = defaultIMJobRetention
	}
	return &IMConfigJobStore{
		db:         db,
		gcInterval: gcInterval,
		retention:  retention,
	}
}

func (s *IMConfigJobStore) EnsureSchema() error {
	return ensureSchemaMigrations(s.db, schemaNameIMConfigJobs, 1, map[int]string{
		1: `
CREATE TABLE IF NOT EXISTS im_config_jobs (
	id TEXT PRIMARY KEY,
	device_id TEXT NOT NULL,
	platform TEXT NOT NULL,
	plugin TEXT,
	status TEXT NOT NULL,
	error TEXT,
	steps_json TEXT NOT NULL,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_im_config_jobs_device_id ON im_config_jobs(device_id);
CREATE INDEX IF NOT EXISTS idx_im_config_jobs_updated_at ON im_config_jobs(updated_at);
`,
	})
}

func (s *IMConfigJobStore) Start() {
	s.lifecycleMu.Lock()
	if s.running {
		s.lifecycleMu.Unlock()
		return
	}
	s.running = true
	s.stopCh = make(chan struct{})
	s.doneCh = make(chan struct{})
	stopCh := s.stopCh
	doneCh := s.doneCh
	interval := s.gcInterval
	s.lifecycleMu.Unlock()

	go s.runGC(stopCh, doneCh, interval)
}

func (s *IMConfigJobStore) Stop() {
	s.lifecycleMu.Lock()
	if !s.running {
		s.lifecycleMu.Unlock()
		return
	}
	stopCh := s.stopCh
	doneCh := s.doneCh
	s.running = false
	s.stopCh = nil
	s.doneCh = nil
	s.lifecycleMu.Unlock()

	close(stopCh)
	<-doneCh
}

func (s *IMConfigJobStore) Close() {
	s.Stop()
}

func (s *IMConfigJobStore) runGC(stopCh <-chan struct{}, doneCh chan<- struct{}, interval time.Duration) {
	defer close(doneCh)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			if _, err := s.DeleteOlderThan(nowUnix() - int64(s.retention/time.Second)); err != nil {
				log.Printf("im-config job gc error: %v", err)
			}
		}
	}
}

func (s *IMConfigJobStore) Create(deviceID, platform string, steps []IMConfigStep) (IMConfigJob, error) {
	id, err := randomHex(16)
	if err != nil {
		return IMConfigJob{}, err
	}

	now := nowUnix()
	job := IMConfigJob{
		ID:        id,
		DeviceID:  deviceID,
		Platform:  platform,
		Status:    "queued",
		Steps:     copyIMConfigSteps(steps),
		CreatedAt: now,
		UpdatedAt: now,
	}

	stepsJSON, err := json.Marshal(job.Steps)
	if err != nil {
		return IMConfigJob{}, err
	}
	if _, err := s.db.Exec(`
INSERT INTO im_config_jobs(id, device_id, platform, plugin, status, error, steps_json, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?);
`, job.ID, job.DeviceID, job.Platform, job.Plugin, job.Status, job.Error, string(stepsJSON), job.CreatedAt, job.UpdatedAt); err != nil {
		return IMConfigJob{}, err
	}

	return copyIMConfigJob(job), nil
}

func (s *IMConfigJobStore) Get(deviceID, id string) (IMConfigJob, error) {
	job, err := s.getByID(id)
	if err != nil {
		return IMConfigJob{}, err
	}
	if job.DeviceID != deviceID {
		return IMConfigJob{}, ErrNotFound
	}
	return job, nil
}

func (s *IMConfigJobStore) Update(id string, mutate func(job *IMConfigJob)) error {
	if mutate == nil {
		return errors.New("mutate callback is required")
	}

	s.updateMu.Lock()
	defer s.updateMu.Unlock()

	job, err := s.getByID(id)
	if err != nil {
		return err
	}
	mutate(&job)
	job.UpdatedAt = nowUnix()

	stepsJSON, err := json.Marshal(job.Steps)
	if err != nil {
		return err
	}

	res, err := s.db.Exec(`
UPDATE im_config_jobs
SET platform = ?, plugin = ?, status = ?, error = ?, steps_json = ?, updated_at = ?
WHERE id = ?;
`, job.Platform, job.Plugin, job.Status, job.Error, string(stepsJSON), job.UpdatedAt, job.ID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *IMConfigJobStore) DeleteOlderThan(cutoffUnix int64) (int64, error) {
	res, err := s.db.Exec(`
DELETE FROM im_config_jobs
WHERE COALESCE(updated_at, created_at) < ?;
`, cutoffUnix)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *IMConfigJobStore) getByID(id string) (IMConfigJob, error) {
	var job IMConfigJob
	var stepsJSON string
	if err := s.db.QueryRow(`
SELECT id, device_id, platform, plugin, status, error, steps_json, created_at, updated_at
FROM im_config_jobs
WHERE id = ?;
`, id).Scan(
		&job.ID,
		&job.DeviceID,
		&job.Platform,
		&job.Plugin,
		&job.Status,
		&job.Error,
		&stepsJSON,
		&job.CreatedAt,
		&job.UpdatedAt,
	); err != nil {
		if err == sql.ErrNoRows {
			return IMConfigJob{}, ErrNotFound
		}
		return IMConfigJob{}, err
	}
	if stepsJSON != "" {
		if err := json.Unmarshal([]byte(stepsJSON), &job.Steps); err != nil {
			return IMConfigJob{}, err
		}
	}
	return copyIMConfigJob(job), nil
}

func copyIMConfigJob(in IMConfigJob) IMConfigJob {
	out := in
	out.Steps = copyIMConfigSteps(in.Steps)
	return out
}

func copyIMConfigSteps(in []IMConfigStep) []IMConfigStep {
	out := make([]IMConfigStep, len(in))
	for i := range in {
		out[i] = in[i]
		if in[i].Record != nil {
			rec := *in[i].Record
			rec.Args = append([]string(nil), in[i].Record.Args...)
			out[i].Record = &rec
		}
	}
	return out
}

func randomHex(size int) (string, error) {
	if size <= 0 {
		return "", errors.New("invalid random size")
	}
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
