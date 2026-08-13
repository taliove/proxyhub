package store

// migrateAirportHosts adds the hosts column to airports (issue #116 上游 hosts 保真).
// 与 migrateEndpointPublicName 同一先例:新库由 store.go 内联 schema 建出,
// 既有库靠本函数幂等补列。空串 = 无 hosts 映射。
func (s *Store) migrateAirportHosts() error {
	return s.addColumnIfMissing("airports", "hosts", "TEXT NOT NULL DEFAULT ''")
}
