package probe

type Status string

const (
	StatusPass Status = "pass"
	StatusWarn Status = "warn"
	StatusFail Status = "fail"
)

type Result struct {
	Suite      string         `json:"suite"`
	Status     Status         `json:"status"`
	Summary    string         `json:"summary"`
	Findings   []string       `json:"findings,omitempty"`
	Metrics    map[string]any `json:"metrics,omitempty"`
	Error      string         `json:"error,omitempty"`
	DurationMS int64          `json:"duration_ms"`
}

type RunConfig struct {
	Model                   string
	BlockStartBytes         int
	BlockMaxBytes           int
	MaxToolRounds           int
	DeepProbe               bool
	ForensicsLevel          string
	ConsistencyRuns         int
	ConsistencyDriftWarn    float64
	ConsistencyDriftFail    float64
	EnableTrustScore        bool
	HardGate                bool
	HardGateStreamFail      bool
	HardGateErrorFail       bool
	HardGateSpoofRisk       float64
	ScoreWeightAuthenticity float64
	ScoreWeightInjection    float64
	ScoreWeightTools        float64
	ScoreWeightToolChoice   float64
	ScoreWeightStream       float64
	ScoreWeightError        float64
	ScoreWeightLatency      float64
	LatencyRounds           int
	ScoreWarnThreshold      float64
	ScoreFailThreshold      float64
	ReasoningBankPath       string
	ReasoningRepeat         int
	ReasoningDomains        string
	ReasoningMaxCases       int
	ReasoningDomainWarn     float64
	ReasoningDomainFail     float64
	ReasoningWeightedWarn   float64
	ReasoningWeightedFail   float64
	NeedleStartBytes        int
	NeedleMaxBytes          int
	NeedleRunsPerPos        int
	IdentityRounds          int
	IdentitySeed            int64
	ScoreWeightIdentity     float64
}

type Report struct {
	GeneratedAt string   `json:"generated_at"`
	Endpoint    string   `json:"endpoint"`
	Model       string   `json:"model"`
	Results     []Result `json:"results"`
	Passed      int      `json:"passed"`
	Warned      int      `json:"warned"`
	Failed      int      `json:"failed"`
}
