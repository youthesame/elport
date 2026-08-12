from pathlib import Path

import pytest

from elport.transclude import (
    download_url,
    extract,
    parse_download_url,
    plan,
    replace_spans,
    reverse,
    safe_name,
)


def test_extracts_each_markdown_and_html_notation():
    text = (
        "![image](image.png) [data](data.csv) "
        "<img src=\"figure.png\"> <a href='note.pdf'>note</a>"
    )
    assert [reference.path for reference in extract(text)] == [
        "image.png",
        "data.csv",
        "figure.png",
        "note.pdf",
    ]


def test_html_attribute_names_must_be_exact():
    text = (
        '<img data-src="data.png" not-src="not.png" _src="under.png" '
        'src="image.png"> <a data-href="meta.pdf" href="note.pdf">note</a>'
    )

    assert [reference.path for reference in extract(text)] == [
        "image.png",
        "note.pdf",
    ]


def test_html_attributes_are_only_extracted_inside_tags(tmp_path: Path):
    note = tmp_path / "notes.txt"
    note.write_text("notes", encoding="utf-8")
    text = (
        '<p>The href="notes.txt" and src="notes.txt" options...</p> '
        '<a href="notes.txt">notes</a>'
    )

    references = plan(text, tmp_path)

    assert [reference.path for reference in references] == ["notes.txt"]
    assert replace_spans(text, references, {note: "URL"}) == (
        '<p>The href="notes.txt" and src="notes.txt" options...</p> '
        '<a href="URL">notes</a>'
    )


def test_html_attribute_entities_are_unescaped_before_path_resolution(tmp_path: Path):
    image = tmp_path / "a&b.png"
    image.write_bytes(b"image")
    text = '<img src="a&amp;b.png">'

    references = plan(text, tmp_path)

    assert references[0].path == "a&b.png"
    assert references[0].file == image.resolve()
    assert replace_spans(text, references, {image: "URL"}) == '<img src="URL">'


def test_html_tag_scanner_allows_greater_than_inside_quoted_attribute(
    tmp_path: Path,
):
    image = tmp_path / "figure.png"
    image.write_bytes(b"image")
    text = '<img alt="signal > blank" src="figure.png">'

    references = plan(text, tmp_path)

    assert [reference.path for reference in references] == ["figure.png"]
    assert replace_spans(text, references, {image: "URL"}) == (
        '<img alt="signal > blank" src="URL">'
    )


def test_markdown_destination_inside_html_tag_is_not_extracted(tmp_path: Path):
    secret = tmp_path / "secret.txt"
    secret.write_text("secret", encoding="utf-8")
    pre_file = tmp_path / "pre file.txt"
    pre_file.write_text("secret", encoding="utf-8")
    image = tmp_path / "figure.png"
    image.write_bytes(b"image")
    text = '<img alt="[example](secret.txt) [angle](<pre file.txt>)" src="figure.png">'

    references = plan(text, tmp_path)

    assert [reference.path for reference in references] == ["figure.png"]
    assert replace_spans(text, references, {image: "URL"}) == (
        '<img alt="[example](secret.txt) [angle](<pre file.txt>)" src="URL">'
    )


def test_angle_bracket_destination_allows_spaces_and_parentheses(tmp_path: Path):
    attachment = tmp_path / "図 1 (テスト).png"
    attachment.write_bytes(b"image")
    text = "[plot](<図 1 (テスト).png>)"

    references = plan(text, tmp_path)

    assert [reference.path for reference in references] == ["図 1 (テスト).png"]
    assert references[0].file == attachment.resolve()
    assert replace_spans(text, references, {attachment: "URL"}) == "[plot](URL)"


def test_angle_bracket_destination_preserves_parenthesized_title(tmp_path: Path):
    attachment = tmp_path / "図 1.png"
    attachment.write_bytes(b"image")
    text = "[plot](<図 1.png> (Figure 1))"

    references = plan(text, tmp_path)

    assert [reference.path for reference in references] == ["図 1.png"]
    assert references[0].file == attachment.resolve()
    assert replace_spans(text, references, {attachment: "URL"}) == (
        "[plot](URL (Figure 1))"
    )


@pytest.mark.parametrize(
    ("text", "filename"),
    [
        ("[plot](<figure 1.png>)", "figure 1.png"),
        ("[x](<pre file.txt>)", "pre file.txt"),
    ],
)
def test_angle_bracket_destination_starting_with_html_tag_name(
    tmp_path: Path, text: str, filename: str
):
    attachment = tmp_path / filename
    attachment.write_bytes(b"data")

    references = plan(text, tmp_path)

    assert [reference.path for reference in references] == [filename]
    assert references[0].file == attachment.resolve()
    assert replace_spans(text, references, {attachment: "URL"}) == text.replace(
        f"<{filename}>", "URL"
    )


@pytest.mark.parametrize(
    ("text", "filename"),
    [
        ("[plot](plot(1).png)", "plot(1).png"),
        (r"[plot](plot\(1\).png)", "plot(1).png"),
        ("[plot](plot((1)).png)", "plot((1)).png"),
    ],
)
def test_bare_destination_allows_balanced_or_escaped_parentheses(
    tmp_path: Path, text: str, filename: str
):
    attachment = tmp_path / filename
    attachment.write_bytes(b"image")

    references = plan(text, tmp_path)

    assert [reference.path for reference in references] == [filename]
    assert references[0].file == attachment.resolve()
    assert replace_spans(text, references, {attachment: "URL"}) == "[plot](URL)"


def test_balanced_bare_destination_preserves_link_title(tmp_path: Path):
    attachment = tmp_path / "plot(1).png"
    attachment.write_bytes(b"image")
    text = '[plot](plot(1).png "Figure 1")'

    references = plan(text, tmp_path)

    assert references[0].file == attachment.resolve()
    assert replace_spans(text, references, {attachment: "URL"}) == (
        '[plot](URL "Figure 1")'
    )


def test_fragment_is_preserved_when_local_path_is_replaced(tmp_path: Path):
    attachment = tmp_path / "manual.pdf"
    attachment.write_bytes(b"pdf")
    text = "[manual](manual.pdf#page=4)"

    references = plan(text, tmp_path)

    assert references[0].file == attachment.resolve()
    assert replace_spans(text, references, {attachment: "URL"}) == (
        "[manual](URL#page=4)"
    )


def test_query_reference_is_not_uploaded_and_warns(tmp_path: Path, capsys):
    attachment = tmp_path / "manual.pdf"
    attachment.write_bytes(b"pdf")

    reference = plan("[manual](manual.pdf?download=1)", tmp_path)[0]

    assert reference.file is None
    assert "manual.pdf?download=1" in capsys.readouterr().err


def test_dotfile_reference_is_not_uploaded_and_warns(tmp_path: Path, capsys):
    (tmp_path / ".env").write_text("SECRET=value", encoding="utf-8")

    reference = plan("[x](.env)", tmp_path)[0]

    assert reference.file is None
    assert "local reference not uploaded" in capsys.readouterr().err


def test_file_in_hidden_directory_is_not_uploaded_and_warns(tmp_path: Path, capsys):
    (tmp_path / ".git").mkdir()
    (tmp_path / ".git" / "config").write_text("secret", encoding="utf-8")

    reference = plan("[x](.git/config)", tmp_path)[0]

    assert reference.file is None
    assert "local reference not uploaded" in capsys.readouterr().err


def test_file_through_hidden_symlink_is_not_uploaded_and_warns(tmp_path: Path, capsys):
    (tmp_path / "shared" / "aws").mkdir(parents=True)
    (tmp_path / "shared" / "aws" / "credentials").write_text("secret", encoding="utf-8")
    (tmp_path / ".aws").symlink_to("shared/aws", target_is_directory=True)

    reference = plan("[x](.aws/credentials)", tmp_path)[0]

    assert reference.file is None
    assert "local reference not uploaded" in capsys.readouterr().err


def test_regular_file_reference_is_still_uploaded(tmp_path: Path):
    attachment = tmp_path / "figure.png"
    attachment.write_bytes(b"image")

    reference = plan("[real](figure.png)", tmp_path)[0]

    assert reference.file is not None


def test_explicit_relative_file_reference_is_still_uploaded(tmp_path: Path):
    attachment = tmp_path / "figure.png"
    attachment.write_bytes(b"image")

    reference = plan("[real](./figure.png)", tmp_path)[0]

    assert reference.file is not None


def test_existing_reference_definition_warns_that_it_is_not_uploaded(
    tmp_path: Path, capsys
):
    (tmp_path / "results.png").write_bytes(b"image")

    assert plan("[x][fig]\n\n[fig]: results.png\n", tmp_path) == []

    assert "reference-style link not uploaded" in capsys.readouterr().err


@pytest.mark.parametrize(
    "definition",
    ["[x]: https://example.org/a.png", "[y]: missing.png"],
)
def test_nonlocal_reference_definition_does_not_warn(
    tmp_path: Path, capsys, definition: str
):
    plan(definition, tmp_path)

    assert "reference-style link not uploaded" not in capsys.readouterr().err


def test_reference_definition_inside_fence_does_not_warn(tmp_path: Path, capsys):
    (tmp_path / "results.png").write_bytes(b"image")

    plan("```markdown\n[fig]: results.png\n```\n", tmp_path)

    assert "reference-style link not uploaded" not in capsys.readouterr().err


def test_escaped_markdown_link_is_not_extracted(tmp_path: Path):
    (tmp_path / "secret.txt").write_text("secret", encoding="utf-8")

    assert plan(r"\[example](secret.txt)", tmp_path) == []


@pytest.mark.parametrize(
    "target",
    [
        "http://example.test/a",
        "https://example.test/a",
        "data:image/png;base64,AA",
        "mailto:user@example.test",
        "#section",
    ],
)
def test_scheme_and_anchor_references_are_not_uploads(tmp_path: Path, target: str):
    reference = plan(f"[x]({target})", tmp_path)[0]
    assert reference.file is None


def test_only_existing_files_are_targets(tmp_path: Path):
    existing = tmp_path / "data.csv"
    existing.write_text("x")
    references = plan("[yes](data.csv) [no](missing.csv)", tmp_path)
    assert references[0].file == existing.resolve()
    assert references[1].file is None


def test_multiple_references_resolve_to_one_upload_target(tmp_path: Path):
    file_path = tmp_path / "figure.png"
    file_path.write_bytes(b"x")
    references = plan('[a](figure.png) <img src="figure.png">', tmp_path)
    assert len({reference.file for reference in references if reference.file}) == 1


def test_basename_collision_is_an_error(tmp_path: Path):
    (tmp_path / "a").mkdir()
    (tmp_path / "b").mkdir()
    (tmp_path / "a" / "x.dat").write_text("a")
    (tmp_path / "b" / "x.dat").write_text("b")
    with pytest.raises(ValueError, match="basename collision"):
        plan("[a](a/x.dat) [b](b/x.dat)", tmp_path)


def test_basename_collision_is_case_insensitive(tmp_path: Path):
    (tmp_path / "a").mkdir()
    (tmp_path / "b").mkdir()
    (tmp_path / "a" / "X.dat").write_text("a")
    (tmp_path / "b" / "x.dat").write_text("b")

    with pytest.raises(ValueError, match="basename collision"):
        plan("[a](a/X.dat) [b](b/x.dat)", tmp_path)


def test_code_comments_pre_and_code_are_not_parsed(tmp_path: Path):
    file_path = tmp_path / "figure.png"
    file_path.write_bytes(b"x")
    text = """[real](figure.png)
`[single](figure.png)` ``[double](figure.png)``
```
[fenced](figure.png)
```
~~~
[tilde](figure.png)
~~~
<!-- [comment](figure.png) -->
<pre>[pre](figure.png)</pre>
<code>[code](figure.png)</code>
"""
    references = plan(text, tmp_path)
    assert [reference.path for reference in references] == ["figure.png"]


def test_crlf_fenced_block_round_trip_preserves_code_and_later_attachment(
    tmp_path: Path,
):
    attachment = tmp_path / "figure.png"
    attachment.write_bytes(b"image")
    upload = {
        "id": 4,
        "long_name": "x/y",
        "real_name": "figure.png",
        "storage": 1,
    }
    url = download_url("https://e.example", upload)
    local = "```markdown\r\n[example](figure.png)\r\n```\r\n[actual](figure.png)\r\n"

    remote = replace_spans(local, plan(local, tmp_path), {attachment: url})
    restored, used = reverse(remote, [upload], "https://e.example")

    assert remote == (
        f"```markdown\r\n[example](figure.png)\r\n```\r\n[actual]({url})\r\n"
    )
    assert restored == local
    assert used == [upload]


@pytest.mark.parametrize("ticks", ["```", "````"])
def test_long_inline_code_spans_are_not_parsed(tmp_path: Path, ticks: str):
    (tmp_path / "secret.txt").write_text("secret", encoding="utf-8")

    references = plan(f"prefix {ticks}[secret](secret.txt){ticks} suffix", tmp_path)

    assert references == []


def test_double_backtick_span_may_contain_single_backticks(tmp_path: Path):
    (tmp_path / "secret.txt").write_text("secret", encoding="utf-8")

    assert plan("`` `[secret](secret.txt)` ``", tmp_path) == []


def test_mismatched_backtick_runs_do_not_form_code_span(tmp_path: Path):
    secret = tmp_path / "secret.txt"
    secret.write_text("secret", encoding="utf-8")

    references = plan("prefix ```[secret](secret.txt)```` suffix", tmp_path)

    assert references[0].file == secret.resolve()


def test_fenced_backtick_does_not_consume_later_inline_opener(tmp_path: Path):
    (tmp_path / "secret.txt").write_text("secret", encoding="utf-8")
    real = tmp_path / "real.txt"
    real.write_text("real", encoding="utf-8")
    text = "~~~\n`\n~~~\n`[hidden](secret.txt)`\n[real](real.txt)"

    references = plan(text, tmp_path)

    assert [reference.file for reference in references] == [real.resolve()]


@pytest.mark.parametrize("fence", ["```python", "~~~"])
def test_unterminated_fence_is_masked_through_eof(tmp_path: Path, fence: str):
    file_path = tmp_path / "figure.png"
    file_path.write_bytes(b"x")

    references = plan(f"[real](figure.png)\n{fence}\n[code](figure.png)\n", tmp_path)

    assert [reference.path for reference in references] == ["figure.png"]


@pytest.mark.parametrize(
    "opener",
    ["<!--", "<pre>", "<PRE class='sample'>", "<code class='sample'>", "<CoDe>"],
)
def test_unterminated_html_excluded_region_is_masked_through_eof(
    tmp_path: Path, opener: str
):
    file_path = tmp_path / "figure.png"
    file_path.write_bytes(b"x")

    references = plan(f"[real](figure.png)\n{opener}\n[hidden](figure.png)\n", tmp_path)

    assert [reference.path for reference in references] == ["figure.png"]


@pytest.mark.parametrize("opener", ["<!--", "<pre>", "<code>"])
@pytest.mark.parametrize(
    "excluded",
    ["```\n{opener}\n```", "`{opener}`", "<!-- {opener} -->"],
)
def test_html_opener_in_excluded_region_does_not_mask_later_reference(
    tmp_path: Path, opener: str, excluded: str
):
    file_path = tmp_path / "figure.png"
    file_path.write_bytes(b"x")

    references = plan(
        excluded.format(opener=opener) + "\n[real](figure.png)\n", tmp_path
    )

    assert [reference.file for reference in references] == [file_path.resolve()]


@pytest.mark.parametrize("pattern", ["scratch/", "scratch/**"])
def test_directory_ignore_patterns_exclude_nested_files(tmp_path: Path, pattern: str):
    nested = tmp_path / "scratch" / "a" / "data.csv"
    nested.parent.mkdir(parents=True)
    nested.write_text("secret", encoding="utf-8")

    reference = plan("[data](scratch/a/data.csv)", tmp_path, [pattern])[0]

    assert reference.file is None


def test_trailing_slash_ignore_matches_nested_directory_segment(tmp_path: Path):
    nested = tmp_path / "work" / "scratch" / "data.csv"
    nested.parent.mkdir(parents=True)
    nested.write_text("secret", encoding="utf-8")

    reference = plan("[data](work/scratch/data.csv)", tmp_path, ["scratch/"])[0]

    assert reference.file is None


@pytest.mark.parametrize("relative", ["scratch/data.csv", "work/scratch/data.csv"])
def test_bare_directory_ignore_matches_path_segments(tmp_path: Path, relative: str):
    nested = tmp_path / relative
    nested.parent.mkdir(parents=True)
    nested.write_text("secret", encoding="utf-8")

    reference = plan(f"[data]({relative})", tmp_path, ["scratch"])[0]

    assert reference.file is None


def test_bare_ignore_respects_segment_boundaries_and_nested_filenames(tmp_path: Path):
    included = tmp_path / "scratchpad" / "data.csv"
    ignored = tmp_path / "work" / "secret.tmp"
    for path in (included, ignored):
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("data", encoding="utf-8")

    references = plan(
        "[included](scratchpad/data.csv) [ignored](work/secret.tmp)",
        tmp_path,
        ["scratch", "*.tmp"],
    )

    assert references[0].file == included.resolve()
    assert references[1].file is None


def test_globstar_matches_zero_or_multiple_directories(tmp_path: Path):
    paths = [
        tmp_path / "results" / "private" / "zero.csv",
        tmp_path / "results" / "a" / "b" / "private" / "many.csv",
    ]
    for path in paths:
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("secret", encoding="utf-8")

    references = plan(
        "[zero](results/private/zero.csv) [many](results/a/b/private/many.csv)",
        tmp_path,
        ["results/**/private/*.csv"],
    )

    assert all(reference.file is None for reference in references)


def test_single_star_does_not_match_across_directories(tmp_path: Path):
    direct = tmp_path / "results" / "direct.csv"
    nested = tmp_path / "results" / "run1" / "data.csv"
    for path in (direct, nested):
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("data", encoding="utf-8")

    references = plan(
        "[direct](results/direct.csv) [nested](results/run1/data.csv)",
        tmp_path,
        ["results/*.csv"],
    )

    assert references[0].file is None
    assert references[1].file == nested.resolve()


def test_directory_ignore_requires_path_segment_boundary(tmp_path: Path):
    path = tmp_path / "scratchpad" / "data.csv"
    path.parent.mkdir()
    path.write_text("keep", encoding="utf-8")

    reference = plan("[data](scratchpad/data.csv)", tmp_path, ["scratch/"])[0]

    assert reference.file == path.resolve()


@pytest.mark.parametrize(
    ("pattern", "relative"),
    [
        ("**/scratch/", "work/scratch/data.csv"),
        ("private/**/", "private/a/b/data.csv"),
    ],
)
def test_globstar_directory_patterns_exclude_descendants(
    tmp_path: Path, pattern: str, relative: str
):
    path = tmp_path / relative
    path.parent.mkdir(parents=True)
    path.write_text("secret", encoding="utf-8")

    reference = plan(f"[data]({relative})", tmp_path, [pattern])[0]

    assert reference.file is None


@pytest.mark.parametrize(
    ("pattern", "relative"),
    [("/secret.txt", "secret.txt"), ("/scratch/", "scratch/data.csv")],
)
def test_root_anchored_ignore_patterns(tmp_path: Path, pattern: str, relative: str):
    path = tmp_path / relative
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text("secret", encoding="utf-8")

    reference = plan(f"[data]({relative})", tmp_path, [pattern])[0]

    assert reference.file is None


@pytest.mark.parametrize("ticks", ["`", "``"])
def test_multiline_inline_code_is_masked(tmp_path: Path, ticks: str):
    path = tmp_path / "secret.txt"
    path.write_text("secret", encoding="utf-8")

    references = plan(f"{ticks}example\n[secret](secret.txt)\ncode{ticks}", tmp_path)

    assert references == []


def test_absolute_parent_and_symlink_escapes_are_rejected(tmp_path: Path):
    outside = tmp_path.parent / f"{tmp_path.name}-outside"
    outside.write_text("secret")
    link = tmp_path / "link"
    link.symlink_to(outside)
    text = f"[absolute]({outside}) [parent](../{outside.name}) [link](link)"
    assert all(reference.file is None for reference in plan(text, tmp_path))


def test_fragment_symlink_escape_warns_and_is_rejected(tmp_path: Path, capsys):
    document_dir = tmp_path / "doc"
    document_dir.mkdir()
    outside = tmp_path / "outside.txt"
    outside.write_text("secret", encoding="utf-8")
    link = document_dir / "link.txt"
    link.symlink_to(outside)

    reference = plan("[link](link.txt#part)", document_dir)[0]

    assert reference.file is None
    assert "link.txt#part" in capsys.readouterr().err


def test_invalid_local_references_warn_but_intentional_exclusions_do_not(
    tmp_path: Path, capsys
):
    document_dir = tmp_path / "doc"
    document_dir.mkdir()
    outside = tmp_path / "outside.txt"
    outside.write_text("secret", encoding="utf-8")
    ignored = document_dir / "ignored.bin"
    ignored.write_bytes(b"ignored")
    link = document_dir / "link.txt"
    link.symlink_to(outside)
    text = (
        "[missing](missing.txt) [absolute](/tmp/absolute.txt) "
        "[parent](../outside.txt) [link](link.txt) "
        "[web](https://example.test/x) [plain](http://example.test/x) "
        "[mail](mailto:user@example.test) [data](data:text/plain,x) "
        "[anchor](#part) [ignored](ignored.bin)"
    )

    plan(text, document_dir, ["ignored.bin"])

    warning = capsys.readouterr().err
    for path in ("missing.txt", "/tmp/absolute.txt", "../outside.txt", "link.txt"):
        assert path in warning
    assert "https://example.test/x" not in warning
    assert "http://example.test/x" not in warning
    assert "mailto:user@example.test" not in warning
    assert "data:text/plain,x" not in warning
    assert "#part" not in warning
    assert "ignored.bin" not in warning


def test_download_url_percent_encoding_and_query_order_independence():
    upload = {"long_name": "a/b c", "real_name": "図 1&x", "storage": 1}
    url = download_url("https://e.example", upload)
    assert "%2F" in url and "%26" in url
    assert parse_download_url(url) == ("a/b c", "図 1&x", "1")
    reordered = (
        "https://e.example/app/download.php?storage=1&name=%E5%9B%B3+1%26x&f=a%2Fb+c"
    )
    assert parse_download_url(reordered) == ("a/b c", "図 1&x", "1")


def test_download_url_literal_percent_sequence_is_decoded_once():
    upload = {
        "id": 4,
        "long_name": "folder/%20.dat",
        "real_name": "literal%20name.txt",
        "storage": 1,
    }
    url = download_url("https://e.example", upload)

    assert parse_download_url(url) == (
        "folder/%20.dat",
        "literal%20name.txt",
        "1",
    )
    body, used = reverse(f"[file]({url})", [upload], "https://e.example")
    assert body == "[file](literal%20name.txt)"
    assert used == [upload]


def test_flat_basename_round_trip():
    upload = {"id": 4, "long_name": "x/y", "real_name": "figure.png", "storage": 1}
    url = download_url("https://e.example", upload)
    body, used = reverse(f'<img src="{url}">', [upload], "https://e.example")
    assert body == '<img src="figure.png">'
    assert used == [upload]


@pytest.mark.parametrize(
    "real_name",
    ["図 1 (テスト).png", "section#1.txt"],
)
def test_reverse_markdown_uses_parseable_angle_destination(
    tmp_path: Path, real_name: str
):
    upload = {"id": 4, "long_name": "x/y", "real_name": real_name, "storage": 1}
    url = download_url("https://e.example", upload)
    (tmp_path / real_name).write_bytes(b"data")

    body, used = reverse(f"[file]({url})", [upload], "https://e.example")

    assert body == f"[file](<{real_name}>)"
    references = plan(body, tmp_path)
    assert references[0].file == (tmp_path / real_name).resolve()
    assert used == [upload]


def test_reverse_html_attribute_keeps_plain_attachment_name():
    upload = {
        "id": 4,
        "long_name": "x/y",
        "real_name": "図 1 (テスト).png",
        "storage": 1,
    }
    url = download_url("https://e.example", upload)

    body, used = reverse(f'<img src="{url}">', [upload], "https://e.example")

    assert body == '<img src="図 1 (テスト).png">'
    assert used == [upload]


def test_reverse_preserves_download_url_fragment():
    upload = {
        "id": 4,
        "long_name": "x/y",
        "real_name": "manual.pdf",
        "storage": 1,
    }
    url = download_url("https://e.example", upload)

    body, used = reverse(f"[manual]({url}#page=4)", [upload], "https://e.example")

    assert body == "[manual](manual.pdf#page=4)"
    assert used == [upload]


def test_span_replacement_does_not_touch_fence_or_partial_match(tmp_path: Path):
    figure = tmp_path / "figure.png"
    other = tmp_path / "myfigure.png"
    figure.write_bytes(b"x")
    other.write_bytes(b"y")
    text = "[a](figure.png)\n```\n[a](figure.png)\n```\n[b](myfigure.png)"
    rewritten = replace_spans(text, plan(text, tmp_path), {figure: "URL"})
    assert rewritten == "[a](URL)\n```\n[a](figure.png)\n```\n[b](myfigure.png)"


def test_html_attribute_whitespace_is_excluded_from_replacement_span(tmp_path: Path):
    figure = tmp_path / "figure.png"
    figure.write_bytes(b"x")
    text = '<img src="  figure.png  ">'

    references = plan(text, tmp_path)

    assert references[0].path == "figure.png"
    assert replace_spans(text, references, {figure: "URL"}) == '<img src="  URL  ">'


def test_html_href_trim_preserves_asymmetric_whitespace_and_quotes(tmp_path: Path):
    note = tmp_path / "note.pdf"
    note.write_bytes(b"pdf")
    text = "<a HREF=' note.pdf   '>note</a>"

    references = plan(text, tmp_path)

    assert replace_spans(text, references, {note: "URL"}) == (
        "<a HREF=' URL   '>note</a>"
    )


def test_reverse_does_not_replace_urls_in_excluded_ranges():
    upload = {"id": 4, "long_name": "x/y", "real_name": "figure.png", "storage": 1}
    url = download_url("https://e.example", upload)
    text = f"[real]({url})\n```\n[example]({url})\n```\n`{url}`"
    body, used = reverse(text, [upload], "https://e.example")
    assert body == f"[real](figure.png)\n```\n[example]({url})\n```\n`{url}`"
    assert used == [upload]


def test_reverse_does_not_replace_url_in_long_inline_code_span():
    upload = {"id": 4, "long_name": "x/y", "real_name": "figure.png", "storage": 1}
    url = download_url("https://e.example", upload)
    text = f"prefix ```[example]({url})``` suffix"

    body, used = reverse(text, [upload], "https://e.example")

    assert body == text
    assert used == []


@pytest.mark.parametrize(
    ("unsafe", "safe"),
    [
        ("../figure.png", "figure.png"),
        ("../../secret", "secret"),
        ("folder\\data.csv", "data.csv"),
        ("~/note.txt", "note.txt"),
    ],
)
def test_real_name_sanitization(unsafe: str, safe: str):
    assert safe_name(unsafe) == safe


@pytest.mark.parametrize("unsafe", ["", ".", "..", "~.."])
def test_dot_only_real_name_uses_attachment_fallback(unsafe: str):
    assert safe_name(unsafe) == "attachment"
