"""Properties the example tests pin down only at a few points.

Run with ``HYPOTHESIS_PROFILE=crosshair`` to explore them symbolically.
"""

from __future__ import annotations

from hypothesis import example, given
from hypothesis import strategies as st

from elport import frontmatter, transclude

nonempty = st.text(min_size=1)
plain = st.text(st.characters(categories=["L", "N"]) | st.sampled_from(" \n"))
path = st.from_regex(
    r"[a-z0-9_-]{1,8}(/[a-z0-9_-]{1,8}){0,2}\.[a-z]{1,4}", fullmatch=True
)


@given(nonempty, nonempty, st.integers(min_value=1, max_value=10**6))
def test_download_url_round_trips(long_name, real_name, storage):
    upload = {"long_name": long_name, "real_name": real_name, "storage": storage}
    url = transclude.download_url("https://e.example/", upload)
    assert transclude.parse_download_url(url) == (long_name, real_name, str(storage))


# PyYAML writes NEL (U+0085) as a raw line break, which reads back as a space.
value = st.text(st.characters(exclude_characters="\x85"))
meta = st.fixed_dictionaries(
    {},
    optional={
        "id": st.integers(min_value=1, max_value=2**31),
        "title": value,
        "profile": value,
        "tags": st.lists(value),
        "read": st.sampled_from(sorted(frontmatter.PERMISSION_LEVELS)),
        "write": st.sampled_from(sorted(frontmatter.PERMISSION_LEVELS)),
        "attachments": st.lists(value.filter(bool)),
    },
)


@given(meta, st.text(), st.booleans())
@example({"title": "実験\u3000"}, "", False)
def test_frontmatter_round_trips(data, body, bom):
    text = frontmatter.render(data, body)
    assert frontmatter.parse("\ufeff" * bom + text) == (data, body)


@given(st.text())
def test_safe_name_is_one_plain_component(name):
    result = transclude.safe_name(name)
    assert "/" not in result and "\\" not in result
    assert result not in ("", ".", "..")


@given(st.text())
def test_masking_only_blanks(text):
    masked = transclude._masked(text)
    assert len(masked) == len(text)
    assert all(m == t or m == " " for m, t in zip(masked, text))


@given(
    plain,
    plain,
    path,
    st.sampled_from(["```\n{}\n```\n", "~~~\n{}\n~~~\n", "`{}`", "<!-- {} -->"]),
)
def test_code_and_comments_hide_references(before, after, target, wrapper):
    link = wrapper.format(f"![x]({target}) <img src='{target}'>")
    assert transclude.extract(f"{before}\n\n{link}\n\n{after}") == []
