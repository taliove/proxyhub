-- 007: Detection targets support
-- Adds target_name dimension to node_health for multi-dimensional unlock detection

-- Add target_name to node_health (default 'connectivity' for existing rows)
ALTER TABLE node_health ADD COLUMN target_name TEXT NOT NULL DEFAULT 'connectivity';

-- Create composite index for querying per-node per-target results
CREATE INDEX idx_node_health_node_target ON node_health(node_key, target_name, checked_at DESC);

-- Insert default detection targets config into settings
INSERT OR IGNORE INTO settings (key, value) VALUES (
  'detection_targets',
  '[
    {
      "name": "connectivity",
      "url": "http://www.gstatic.com/generate_204",
      "method": "GET",
      "headers": {},
      "expect_status": [204],
      "response_contains": [],
      "response_excludes": []
    },
    {
      "name": "Google",
      "url": "https://www.google.com/search?q=test",
      "method": "GET",
      "headers": {},
      "expect_status": [200],
      "response_contains": [],
      "response_excludes": ["unusual traffic"]
    },
    {
      "name": "OpenAI",
      "url": "https://chat.openai.com",
      "method": "GET",
      "headers": {},
      "expect_status": [200, 403],
      "response_contains": [],
      "response_excludes": ["unsupported_country", "VPN or proxy"]
    },
    {
      "name": "YouTube",
      "url": "https://www.youtube.com",
      "method": "GET",
      "headers": {},
      "expect_status": [200],
      "response_contains": [],
      "response_excludes": ["not available in your country"]
    },
    {
      "name": "Facebook",
      "url": "https://www.facebook.com",
      "method": "GET",
      "headers": {},
      "expect_status": [200],
      "response_contains": [],
      "response_excludes": ["not available in your region"]
    },
    {
      "name": "Claude",
      "url": "https://claude.ai",
      "method": "GET",
      "headers": {},
      "expect_status": [200],
      "response_contains": [],
      "response_excludes": ["app-unavailable-in-region", "not available"]
    }
  ]'
);
