package store

// migrateEndpointLinkReset adds prev_path/prev_token/grace_expires_at to endpoints
// (issue #117 订阅链接重置)。与 migrateAirportHosts 同一先例:新库由 store.go
// 内联 schema 建出,既有库靠本函数幂等补列;空串 = 无宽限。
func (s *Store) migrateEndpointLinkReset() error {
	if err := s.addColumnIfMissing("endpoints", "prev_path", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing("endpoints", "prev_token", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	return s.addColumnIfMissing("endpoints", "grace_expires_at", "TEXT NOT NULL DEFAULT ''")
}
