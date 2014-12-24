CREATE TABLE tvguide (
  start timestamp,
  stop timestamp,
  title varchar(160),
  channel varchar(80),
  description text
);
CREATE TABLE tvguide_favorites (
  id SERIAL PRIMARY KEY,
  regex varchar(80) NOT NULL,
  channel_regex varchar(80)
);
