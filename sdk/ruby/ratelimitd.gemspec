Gem::Specification.new do |s|
  s.name        = "ratelimitd"
  s.version     = "0.1.0"
  s.summary     = "Ruby client for the ratelimitd rate limiting sidecar"
  s.description = "Standard-library-only client for ratelimitd."
  s.authors     = ["ratelimitd contributors"]
  s.license     = "MIT"
  s.homepage    = "https://github.com/phineup/ratelimitd"
  s.metadata    = { "source_code_uri" => "https://github.com/phineup/ratelimitd/tree/master/sdk/ruby" }
  s.required_ruby_version = ">= 3.0"
  s.files       = Dir["lib/**/*.rb"] + ["README.md"]
  s.require_paths = ["lib"]
end
