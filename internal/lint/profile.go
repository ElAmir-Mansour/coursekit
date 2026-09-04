// Package lint checks a scanned course against publishing requirements.
package lint

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed profiles/*.yaml
var builtinProfiles embed.FS

// VideoRules constrains the picture.
type VideoRules struct {
	// AspectRatios lists acceptable shapes as "16:9" strings. Empty means any.
	AspectRatios []string `yaml:"aspect_ratios"`
	// AspectTolerance is how far a measured ratio may drift and still match.
	AspectTolerance float64 `yaml:"aspect_tolerance"`

	MinWidth  int `yaml:"min_width"`
	MinHeight int `yaml:"min_height"`
	RecWidth  int `yaml:"recommended_width"`
	RecHeight int `yaml:"recommended_height"`

	Codecs []string `yaml:"codecs"`

	MinFPS float64 `yaml:"min_fps"`
	MaxFPS float64 `yaml:"max_fps"`

	MinBitrateKbps int `yaml:"min_bitrate_kbps"`
	RecBitrateKbps int `yaml:"recommended_bitrate_kbps"`
}

// AudioRules constrains the sound, including programme loudness.
type AudioRules struct {
	Required bool     `yaml:"required"`
	Codecs   []string `yaml:"codecs"`

	MinBitrateKbps int   `yaml:"min_bitrate_kbps"`
	RecBitrateKbps int   `yaml:"recommended_bitrate_kbps"`
	Channels       []int `yaml:"channels"`
	SampleRates    []int `yaml:"sample_rates"`

	// TargetLUFS is the programme loudness a platform normalises toward.
	// Checked only when loudness has actually been measured.
	TargetLUFS    float64 `yaml:"target_lufs"`
	LUFSTolerance float64 `yaml:"lufs_tolerance"`
	MaxTruePeak   float64 `yaml:"max_true_peak_dbtp"`
}

// FileRules constrains the container and the name.
type FileRules struct {
	Containers   []string `yaml:"containers"`
	MaxSizeBytes int64    `yaml:"max_size_bytes"`
	MaxSeconds   float64  `yaml:"max_duration_seconds"`

	// PortableNames flags names that are legal on macOS but rejected or
	// mangled elsewhere: trailing spaces, reserved punctuation, and so on.
	PortableNames bool `yaml:"portable_names"`
	// ASCIIOnly additionally flags non-ASCII names, which some uploaders and
	// zip tools still mishandle.
	ASCIIOnly bool `yaml:"ascii_only"`
}

// ConsistencyRules compare files against each other rather than against a
// fixed limit. These catch the problems that only exist across a whole course.
type ConsistencyRules struct {
	UniformResolution bool    `yaml:"uniform_resolution"`
	UniformSampleRate bool    `yaml:"uniform_sample_rate"`
	UniformFrameRate  bool    `yaml:"uniform_frame_rate"`
	UniformCodec      bool    `yaml:"uniform_codec"`
	MaxLoudnessSpread float64 `yaml:"max_loudness_spread_lu"`
	NumberingGaps     bool    `yaml:"numbering_gaps"`
	ChapterOrdering   bool    `yaml:"chapter_ordering"`
}

// Profile is a named set of rules, normally one publishing destination.
type Profile struct {
	Name        string           `yaml:"name"`
	Description string           `yaml:"description"`
	Reference   string           `yaml:"reference"`
	Video       VideoRules       `yaml:"video"`
	Audio       AudioRules       `yaml:"audio"`
	File        FileRules        `yaml:"file"`
	Consistency ConsistencyRules `yaml:"consistency"`
}

// aspectTolerance falls back to a value that separates 16:9 from 16:10
// comfortably while absorbing rounding in odd frame sizes.
func (p *Profile) aspectTolerance() float64 {
	if p.Video.AspectTolerance > 0 {
		return p.Video.AspectTolerance
	}
	return 0.012
}

// BuiltinNames lists the profiles compiled into the binary.
func BuiltinNames() []string {
	entries, err := builtinProfiles.ReadDir("profiles")
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		names = append(names, strings.TrimSuffix(e.Name(), ".yaml"))
	}
	sort.Strings(names)
	return names
}

// UserProfileDir is where a creator's own profiles live. Files there take
// precedence over the built-ins, which is how the "own platform" case is meant
// to be handled: edit a config file, do not patch the tool.
func UserProfileDir() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "coursekit", "profiles")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "coursekit", "profiles")
}

// LoadProfile resolves a profile by name or by path.
//
// A value containing a path separator or ending in .yaml is read as a file, so
// a one-off ruleset can be used without installing it. Otherwise the user's
// profile directory is searched before the built-ins.
func LoadProfile(nameOrPath string) (*Profile, error) {
	if nameOrPath == "" {
		nameOrPath = "udemy"
	}

	if strings.ContainsRune(nameOrPath, filepath.Separator) ||
		strings.HasSuffix(nameOrPath, ".yaml") || strings.HasSuffix(nameOrPath, ".yml") {
		data, err := os.ReadFile(nameOrPath)
		if err != nil {
			return nil, fmt.Errorf("read profile %s: %w", nameOrPath, err)
		}
		return parseProfile(data, nameOrPath)
	}

	if dir := UserProfileDir(); dir != "" {
		for _, ext := range []string{".yaml", ".yml"} {
			path := filepath.Join(dir, nameOrPath+ext)
			if data, err := os.ReadFile(path); err == nil {
				return parseProfile(data, path)
			}
		}
	}

	data, err := builtinProfiles.ReadFile("profiles/" + nameOrPath + ".yaml")
	if err != nil {
		return nil, fmt.Errorf("unknown profile %q (built-in profiles: %s)",
			nameOrPath, strings.Join(BuiltinNames(), ", "))
	}
	return parseProfile(data, nameOrPath)
}

func parseProfile(data []byte, source string) (*Profile, error) {
	var p Profile
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(true)
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("parse profile %s: %w", source, err)
	}
	if p.Name == "" {
		p.Name = strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
	}
	return &p, nil
}

// parseAspect turns "16:9" into 1.777…. A bare decimal is also accepted so a
// profile can express an unusual shape directly.
func parseAspect(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if num, den, found := strings.Cut(s, ":"); found {
		n, err1 := strconv.ParseFloat(strings.TrimSpace(num), 64)
		d, err2 := strconv.ParseFloat(strings.TrimSpace(den), 64)
		if err1 != nil || err2 != nil || d == 0 {
			return 0, fmt.Errorf("bad aspect ratio %q", s)
		}
		return n / d, nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("bad aspect ratio %q", s)
	}
	return f, nil
}

// ProfileSource returns the raw YAML for a profile, so it can be copied as a
// starting point. The source is returned rather than a re-serialised struct
// because the comments in these files explain why each limit is what it is,
// and those are the most useful part to a person writing their own.
func ProfileSource(nameOrPath string) ([]byte, error) {
	if strings.ContainsRune(nameOrPath, filepath.Separator) ||
		strings.HasSuffix(nameOrPath, ".yaml") || strings.HasSuffix(nameOrPath, ".yml") {
		return os.ReadFile(nameOrPath)
	}
	if dir := UserProfileDir(); dir != "" {
		for _, ext := range []string{".yaml", ".yml"} {
			if data, err := os.ReadFile(filepath.Join(dir, nameOrPath+ext)); err == nil {
				return data, nil
			}
		}
	}
	return builtinProfiles.ReadFile("profiles/" + nameOrPath + ".yaml")
}
