package session

// Session represents the metadata of a terminal session. Fields are unexported;
// use constructors and accessors.
type Session struct {
	id           string
	projectID    string
	machineID    string
	title        string
	shell        string
	cwd          string
	status       Status
	exitCode     int
	autoRelaunch bool
	createdAt    int64
	lastActiveAt int64
	activity     string
	// pwd is the session's live working directory (follows cd), refreshed from
	// heartbeats. It is distinct from cwd, which is the fixed spawn directory set
	// at open time and never changes.
	pwd string
}

// New creates a Session at open time. status is StatusRunning; lastActiveAt equals createdAt.
func New(id, machineID, projectID, title, shell, cwd string, createdAt int64) Session {
	return Session{
		id:           id,
		projectID:    projectID,
		machineID:    machineID,
		title:        title,
		shell:        shell,
		cwd:          cwd,
		status:       StatusRunning,
		exitCode:     0,
		autoRelaunch: false,
		createdAt:    createdAt,
		lastActiveAt: createdAt,
	}
}

// Rehydrate reconstructs a Session from a persisted row.
func Rehydrate(id, projectID, machineID, title, shell, cwd string, status Status, exitCode int, autoRelaunch bool, createdAt, lastActiveAt int64) Session {
	return Session{
		id:           id,
		projectID:    projectID,
		machineID:    machineID,
		title:        title,
		shell:        shell,
		cwd:          cwd,
		status:       status,
		exitCode:     exitCode,
		autoRelaunch: autoRelaunch,
		createdAt:    createdAt,
		lastActiveAt: lastActiveAt,
	}
}

func (s Session) ID() string           { return s.id }
func (s Session) ProjectID() string    { return s.projectID }
func (s Session) MachineID() string    { return s.machineID }
func (s Session) Title() string        { return s.title }
func (s Session) Shell() string        { return s.shell }
func (s Session) Cwd() string          { return s.cwd }
func (s Session) Status() Status       { return s.status }
func (s Session) ExitCode() int        { return s.exitCode }
func (s Session) AutoRelaunch() bool   { return s.autoRelaunch }
func (s Session) CreatedAt() int64     { return s.createdAt }
func (s Session) LastActiveAt() int64  { return s.lastActiveAt }
func (s Session) Activity() string     { return s.activity }
func (s Session) Pwd() string          { return s.pwd }

// SetStatus updates the session status.
func (s *Session) SetStatus(st Status) {
	s.status = st
}

// SetExited marks the session as exited with the given exit code and timestamp.
func (s *Session) SetExited(code int, ts int64) {
	s.status = StatusExited
	s.exitCode = code
	s.lastActiveAt = ts
}

// Touch updates the last-active timestamp.
func (s *Session) Touch(ts int64) {
	s.lastActiveAt = ts
}

// SetTitle updates the session title.
func (s *Session) SetTitle(t string) {
	s.title = t
}

// SetActivity updates the session activity state.
func (s *Session) SetActivity(a string) {
	s.activity = a
}

// SetPwd updates the session's live working directory (follows cd). This is
// distinct from cwd, the fixed spawn directory.
func (s *Session) SetPwd(p string) {
	s.pwd = p
}

// SetAutoRelaunch updates whether this session should be relaunched after agent restart.
func (s *Session) SetAutoRelaunch(v bool) {
	s.autoRelaunch = v
}
