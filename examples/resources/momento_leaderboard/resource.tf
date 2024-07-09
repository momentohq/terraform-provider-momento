resource "momento_cache" "example" {
  name = "cache-name"
}

resource "momento_leaderboard" "example" {
  name       = "leaderboard-name"
  cache_name = momento_cache.example.name
}
