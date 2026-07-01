from kitup import load_host_spec


def test_load_host_spec_uses_baked_default_when_no_override():
    spec = load_host_spec()
    assert spec.schema_version == 1
    assert len(spec.hosts) == 72
    assert spec.hosts[0].id == "adal"
