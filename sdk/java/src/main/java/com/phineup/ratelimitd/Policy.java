package com.phineup.ratelimitd;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;
import java.util.List;

@JsonIgnoreProperties(ignoreUnknown = true)
public record Policy(String name, String description, List<Rule> rules, boolean dynamic) {}
