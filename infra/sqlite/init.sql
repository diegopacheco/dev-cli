CREATE TABLE IF NOT EXISTS hosts (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  region TEXT NOT NULL,
  meta TEXT
);
CREATE TABLE IF NOT EXISTS metrics (
  id INTEGER PRIMARY KEY,
  host_id INTEGER NOT NULL REFERENCES hosts(id),
  name TEXT NOT NULL,
  value REAL NOT NULL,
  labels TEXT
);
DELETE FROM metrics;
DELETE FROM hosts;
INSERT INTO hosts (id, name, region, meta) VALUES
  (1, 'api-1', 'us-east-1', '{"cpu":8,"arch":"arm64","tags":["api","prod"]}'),
  (2, 'api-2', 'us-west-2', '{"cpu":16,"arch":"amd64","tags":["api"]}'),
  (3, 'worker-1', 'eu-central-1', '{"cpu":32,"arch":"arm64","tags":["batch"]}');
INSERT INTO metrics (host_id, name, value, labels) VALUES
  (1, 'cpu_usage', 41.5, '{"unit":"percent"}'),
  (1, 'mem_used', 7.2, '{"unit":"GiB"}'),
  (2, 'cpu_usage', 77.1, '{"unit":"percent"}'),
  (3, 'queue_depth', 1284, '{"queue":"emails","priority":"high"}');
WITH RECURSIVE seq(n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM seq WHERE n < 100)
INSERT INTO metrics (host_id, name, value, labels)
SELECT 1 + n % 3, 'latency_ms', n * 37 % 250, '{"route":"/orders","sample":' || n || '}' FROM seq;
