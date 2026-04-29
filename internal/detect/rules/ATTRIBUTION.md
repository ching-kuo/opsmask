# Detector rule attribution

The built-in detector names and broad regex strategy are based on common public
formats and are inspired by the public Gitleaks rule catalog. The v1
implementation keeps compact, high-precision Go RE2 patterns in source rather
than vendoring the full upstream TOML catalog.
