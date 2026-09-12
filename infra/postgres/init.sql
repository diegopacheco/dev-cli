CREATE TABLE users (
  id SERIAL PRIMARY KEY,
  name TEXT NOT NULL,
  email TEXT NOT NULL,
  profile JSONB,
  created_at TIMESTAMPTZ DEFAULT now()
);
CREATE TABLE orders (
  id SERIAL PRIMARY KEY,
  user_id INT NOT NULL REFERENCES users(id),
  total NUMERIC(10,2) NOT NULL,
  status TEXT NOT NULL,
  items JSONB
);
INSERT INTO users (name, email, profile) VALUES
  ('Ana Ribeiro', 'ana@devcli.io', '{"lang":"go","level":9,"tags":["tui","sql"],"remote":true}'),
  ('Lars Holm', 'lars@devcli.io', '{"lang":"rust","level":7,"tags":["cli"],"remote":false}'),
  ('Mei Tanaka', 'mei@devcli.io', '{"lang":"scala","level":8,"tags":["jvm","akka"],"remote":true}'),
  ('Kofi Mensah', 'kofi@devcli.io', '{"lang":"clojure","level":6,"tags":["repl"],"remote":null}');
INSERT INTO orders (user_id, total, status, items) VALUES
  (1, 129.90, 'paid', '[{"sku":"KB-01","qty":1},{"sku":"MS-02","qty":2}]'),
  (2, 49.00, 'shipped', '[{"sku":"CB-10","qty":3}]'),
  (3, 310.50, 'pending', '[{"sku":"MN-27","qty":1}]'),
  (1, 15.25, 'paid', '[{"sku":"ST-99","qty":5}]');
