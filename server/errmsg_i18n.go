package server

import "github.com/Autumn-27/artex/config"

// trMsg remains as a compatibility boundary for formatting call sites. Messages
// are English literals in this edition; there is no runtime language catalog.
func trMsg(message string) string { return message }

// loadOutputLanguage resolves the user-facing output language from the source of
// truth: the runtime setting (settings table) over config.Language() (ARTEX_LANG >
// config file > ko). Call it only to (re)fill the cache; it does a DB query and a
// config file read.
func (s *Server) loadOutputLanguage() string {
	if s.m != nil && s.m.pg != nil {
		if v, ok, _ := s.m.pg.GetSetting(settingOutputLanguage); ok {
			if nv := config.NormalizeLanguage(v); nv != "" {
				return nv
			}
		}
	}
	return config.Language()
}

// outputLanguage returns the cached output language, resolving it on first use.
func (s *Server) outputLanguage() string {
	if v, ok := s.outputLang.Load().(string); ok && v != "" {
		return v
	}
	v := s.loadOutputLanguage()
	s.outputLang.Store(v)
	return v
}
