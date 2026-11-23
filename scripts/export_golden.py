from __future__ import annotations

import copy
import json
from pathlib import Path
from typing import Any

from elport import frontmatter, transclude

OUT_DIR = Path(__file__).resolve().parent.parent / "testdata" / "golden"


def _utf8_span(text: str, start: int, end: int) -> tuple[int, int]:
    return len(text[:start].encode("utf-8")), len(text[:end].encode("utf-8"))


def _export_frontmatter() -> dict[str, Any]:
    parse_inputs = [
        ("plain_body", "Body\n"),
        ("empty", ""),
        ("non_mapping_text", "---\nIntroduction\n---\nBody\n"),
        ("non_mapping_list", "---\n- one\n- two\n---\nBody\n"),
        ("non_mapping_null", "---\nnull\n---\nBody\n"),
        ("four_dashes", "----\nBody\n"),
        ("no_newline_after_opener", "---not-frontmatter\nBody\n"),
        ("unclosed_opener", "---\nid: 5\nBody\n"),
        ("bom_plain", "\ufeffBody\n"),
        (
            "bom_crlf_preserve_body_bom",
            "\ufeff---\r\nid: 5\r\n---\r\n\r\n\ufeffBody\r\n",
        ),
        (
            "crlf_full",
            "---\r\nid: 42\r\ntitle: Hello\r\n---\r\n\r\nLine 1\r\nLine 2\r\n",
        ),
        ("lone_cr_after_closer", "---\nid: 7\n---\rBody\n"),
        ("trailing_spaces_on_delims", "---   \t\nid: 7\n--- \t \n\nBody\n"),
        (
            "all_allowed_keys",
            (
                "---\nid: 42\nentity: experiments\ntitle: 実験記録 🧪\ntags: [one, 二]\n"
                "category: 3\nstatus: 2\nprofile: lab\nread: team\nwrite: owner\n"
                "attachments: [raw.csv, 123]\n---\n\nBody\n"
            ),
        ),
        (
            "category_status_strings",
            "---\nid: 1\ncategory: Assay\nstatus: Running\n---\nBody\n",
        ),
        ("scalar_tag_str", "---\ntags: PCR\n---\nBody\n"),
        ("scalar_tag_int", "---\ntags: 42\n---\nBody\n"),
        ("scalar_tag_bool", "---\ntags: true\n---\nBody\n"),
        ("tags_null", "---\ntags: null\n---\nBody\n"),
        ("attachments_null", "---\nattachments: null\n---\nBody\n"),
        ("attachments_scalar_str", "---\nattachments: data.csv\n---\nBody\n"),
        ("attachments_scalar_int", "---\nattachments: 100\n---\nBody\n"),
        ("title_int_coerced_to_str", "---\ntitle: 2026\nprofile: 1\n---\nBody\n"),
        (
            "yaml_1_1_yes_no_on_off",
            "---\ntitle: yes\nprofile: on\ntags: [no, off, 017, ~]\n---\nBody\n",
        ),
        (
            "preserve_key_order",
            "---\nprofile: p\ntitle: T\nid: 9\nentity: items\n---\nBody\n",
        ),
        ("empty_body", "---\ntitle: Test\n---\n\n"),
        ("leading_blank_lines_body", "---\ntitle: Test\n---\n\n\n\nBody\n"),
        ("error_unknown_key", "---\ncatetory: x\n---\nBody\n"),
        ("error_id_zero", "---\nid: 0\n---\nBody\n"),
        ("error_id_negative", "---\nid: -1\n---\nBody\n"),
        ("error_id_bool", "---\nid: true\n---\nBody\n"),
        ("error_id_str", "---\nid: '42'\n---\nBody\n"),
        ("error_id_float", "---\nid: 5.0\n---\nBody\n"),
        ("error_duplicate_key", "---\nid: 1\nid: 2\n---\nBody\n"),
        ("error_yaml_alias", "---\ntags: &a [x]\ntitle: *a\n---\nBody\n"),
        (
            "error_deep_nesting",
            "---\ntitle: " + "[" * 500 + "]" * 500 + "\n---\nBody\n",
        ),
        ("error_invalid_read", "---\nread: everyone\n---\nBody\n"),
        ("error_invalid_write", "---\nwrite: 10\n---\nBody\n"),
        ("error_tags_mapping", "---\ntags:\n  assay: PCR\n---\nBody\n"),
        ("error_attachments_bool", "---\nattachments: [true]\n---\nBody\n"),
        ("error_attachments_mapping", "---\nattachments: [{a: 1}]\n---\nBody\n"),
    ]

    parse_cases = []
    for name, text in parse_inputs:
        try:
            meta, body = frontmatter.parse(text)
        except ValueError as exc:
            parse_cases.append({"name": name, "input": text, "error": str(exc)})
        else:
            parse_cases.append(
                {
                    "name": name,
                    "input": text,
                    "meta": meta,
                    "key_order": list(meta.keys()),
                    "body": body,
                }
            )

    render_inputs: list[tuple[str, dict[str, Any], str]] = [
        ("basic", {"title": "Test", "tags": ["one"]}, "Body\n"),
        ("empty_body", {"title": "Test"}, ""),
        ("leading_newlines", {"title": "Test"}, "\n\nBody\n"),
        (
            "key_order_and_unicode",
            {
                "profile": "lab",
                "id": 42,
                "entity": "experiments",
                "title": "温度測定: 300K #1",
                "tags": ["yes", "on", "日本語", "017"],
                "category": 3,
                "status": "Running",
                "read": "team",
                "write": "owner",
                "attachments": ["raw data.csv"],
            },
            "結果\n",
        ),
        ("quoting_traps", {"title": "yes", "profile": "foo: bar"}, "x\n"),
    ]
    render_cases = [
        {
            "name": name,
            "meta": meta,
            "key_order": list(meta.keys()),
            "body": body,
            "output": frontmatter.render(meta, body),
        }
        for name, meta, body in render_inputs
    ]

    text_meta_inputs = [
        ("valid_elport_doc", "---\nid: 5\nprofile: lab\ntitle: T\n---\nBody\n"),
        ("valid_crlf", "---\r\nid: 5\r\nprofile: lab\r\n---\r\nBody\r\n"),
        ("missing_profile", "---\nid: 5\n---\nBody\n"),
        ("empty_profile", "---\nid: 5\nprofile: ''\n---\nBody\n"),
        ("missing_id", "---\nprofile: lab\n---\nBody\n"),
        ("invalid_yaml", "---\nid: [1\n---\nBody\n"),
    ]
    text_meta_cases = [
        {"name": name, "input": text, "meta": frontmatter.text_meta(text)}
        for name, text in text_meta_inputs
    ]

    server_id_inputs: list[tuple[str, Any]] = [
        ("int_positive", 42),
        ("str_positive", "42"),
        ("int_zero", 0),
        ("int_negative", -1),
        ("str_zero", "0"),
        ("str_non_digit", "abc"),
        ("bool_true", True),
        ("none", None),
    ]
    server_id_cases = []
    for name, value in server_id_inputs:
        try:
            parsed = frontmatter.parse_server_id(value)
        except RuntimeError as exc:
            server_id_cases.append({"name": name, "input": value, "error": str(exc)})
        else:
            server_id_cases.append({"name": name, "input": value, "id": parsed})

    return {
        "parse": parse_cases,
        "render": render_cases,
        "text_meta": text_meta_cases,
        "parse_server_id": server_id_cases,
    }


def _export_transclude() -> dict[str, Any]:
    extract_inputs = [
        ("md_image_and_link", "![plot](fig/a.png) and [data](raw.csv)\n"),
        ("md_angle_with_spaces", '![plot](<my fig (1).png> "Title (x)")\n'),
        ("md_balanced_parens", "[file](dir/run(1)/out(2).csv)\n"),
        ("md_escaped_parens_and_space", r"[file](dir/a\ b\(1\).csv)" + "\n"),
        ("md_fragment_and_query", "[sec](other.md#results) [q](a.csv?v=1)\n"),
        ("md_reference_def", '[fig1]: plot.png "Plot"\n[fig2]: <sp ace.png>\n'),
        (
            "html_figure_and_tags",
            (
                '<figure><img src="fig.png" width="80%"><figcaption>図1</figcaption></figure>\n'
                "<a href='doc.pdf'>pdf</a> <source src=clip.mp4>\n"
            ),
        ),
        (
            "html_comments_masked",
            '<!-- ![skip](a.png) <img src="b.png"> -->\n![keep](c.png)\n',
        ),
        (
            "fences_backtick_and_tilde",
            "```md\n![skip1](a.png)\n```\n~~~\n![skip2](b.png)\n~~~\n![keep](c.png)\n",
        ),
        (
            "nested_fence_and_blockquote",
            "> ```\n> ![skip](a.png)\n> ```\n- ~~~\n  ![skip2](b.png)\n  ~~~\n![keep](c.png)\n",
        ),
        ("unclosed_fence", "```\n![skip](a.png)\n![also_skip](b.png)\n"),
        (
            "inline_code_multi_backtick",
            "``![skip](a.png)`` and `![skip2](b.png)` and ![keep](c.png)\n",
        ),
        (
            "raw_text_elements_masked",
            (
                '<pre><img src="skip.png"></pre><script>var x=\'<img src="s.png">\';</script>\n'
                '<style>a{background:url(x.png)}</style>\n<img src="keep.png">\n'
            ),
        ),
        ("angle_code_tag_not_raw_text", "[x](<code>) and ![keep](a.png)\n"),
        (
            "unicode_japanese_fullwidth_emoji",
            '日本語テスト\u3000🧪 ![図表](図/グラフ_1.png) と <img src="データ/結果.svg" width="60%">\n',
        ),
        (
            "python_ascii_control_whitespace",
            "A\x1cB\x1dC\x1eD\x1fE ![img](a.png) [b](<x.png\x1c>)\n",
        ),
    ]

    extract_cases = []
    for name, text in extract_inputs:
        refs = transclude.extract(text)
        items = []
        for r in refs:
            b_start, b_end = _utf8_span(text, r.start, r.end)
            items.append(
                {
                    "path": r.path,
                    "kind": r.kind,
                    "char_start": r.start,
                    "char_end": r.end,
                    "byte_start": b_start,
                    "byte_end": b_end,
                    "slice": text[r.start : r.end],
                }
            )
        extract_cases.append({"name": name, "input": text, "refs": items})

    raw_html_inputs = [
        ("plain_md", "# Heading\n\n![a](a.png)\n"),
        (
            "figure_img",
            '<figure><img src="a.png" width="80%"><figcaption>cap</figcaption></figure>\n',
        ),
        ("sub_sup_br", "H<sub>2</sub>O and x<sup>2</sup><br>\n"),
        ("details_tag", "<details><summary>more</summary>x</details>\n"),
        ("autolink_url", "See <https://example.com> and <user@example.com>\n"),
        ("html_in_fence", '```html\n<figure><img src="a.png"></figure>\n```\n'),
        ("html_in_inline_code", "Use `<figure>` and `<img>` tags.\n"),
        ("html_in_comment", '<!-- <figure><img src="a.png"></figure> -->\n'),
    ]
    raw_html_cases = [
        {"name": name, "input": text, "has_raw_html": transclude.has_raw_html(text)}
        for name, text in raw_html_inputs
    ]

    bare_path_inputs = [
        (
            "mixed_refs",
            "![a](fig/a.png) [u](https://example.com/x) [m](mailto:a@b) [f](#sec) [q](b.csv?x=1#y)\n",
        ),
        ("windows_drive_and_spaces", "[w](C:/tmp/a.csv) [s](<my file.csv>)\n"),
    ]
    bare_path_cases = [
        {"name": name, "input": text, "paths": transclude.bare_paths(text)}
        for name, text in bare_path_inputs
    ]

    ignore_cases = []
    ignore_inputs = [
        ("glob_star", "a.csv", ["*.csv"], ""),
        ("glob_star_nested", "sub/a.csv", ["*.csv"], ""),
        ("anchored_root", "sub/a.csv", ["/a.csv"], ""),
        ("anchored_match", "a.csv", ["/a.csv"], ""),
        ("dir_only", "build/out.png", ["build/"], ""),
        ("dir_only_file_no_match", "build", ["build/"], ""),
        ("doublestar", "a/b/c/d.png", ["a/**/d.png"], ""),
        ("fnmatch_negated_class", "b.csv", ["[!a].csv"], ""),
        ("fnmatch_negated_class_no_match", "a.csv", ["[!a].csv"], ""),
        ("negation_override", "keep.csv", ["*.csv", "!keep.csv"], ""),
        ("layer_prefix", "data.csv", ["sub/*.csv"], "sub"),
    ]
    for name, rel, patterns, prefix in ignore_inputs:
        layer = transclude.IgnoreLayer(patterns=patterns, prefix=prefix)
        ignore_cases.append(
            {
                "name": name,
                "relative": rel,
                "patterns": patterns,
                "prefix": prefix,
                "ignored": transclude._ignored(rel, [layer]),
            }
        )

    base_url = "https://elab.example.test"
    uploads = [
        {"id": 10, "real_name": "image.png", "long_name": "aaa/111.png", "storage": 1},
        {"id": 20, "real_name": "image.png", "long_name": "bbb/222.png", "storage": 1},
        {
            "id": 30,
            "real_name": "raw data (1).csv",
            "long_name": "ccc/333.csv",
            "storage": 2,
        },
        {"id": 40, "real_name": "run#12.csv", "long_name": "ddd/444.csv", "storage": 1},
    ]
    named_uploads = transclude.name_uploads(copy.deepcopy(uploads))

    download_url_cases = [
        {
            "upload": u,
            "url": transclude.download_url(base_url, u),
            "parsed": list(parsed)
            if (
                parsed := transclude.parse_download_url(
                    transclude.download_url(base_url, u)
                )
            )
            else None,
        }
        for u in named_uploads
    ]

    reverse_input = (
        f"![a]({transclude.download_url(base_url, named_uploads[0])})\n"
        f"![b](app/download.php?f=bbb/222.png&name=image.png&storage=1)\n"
        f"[c]({transclude.download_url(base_url, named_uploads[2])})\n"
        f'<img src="{transclude.download_url(base_url, named_uploads[2])}" width="50%">\n'
        f"[hash]({transclude.download_url(base_url, named_uploads[3])})\n"
        f"![other]({base_url}/app/download.php?f=zzz/999.png&name=missing.png&storage=1)\n"
        f"`{transclude.download_url(base_url, named_uploads[0])}`\n"
    )
    rev_text, placed = transclude.reverse(reverse_input, named_uploads, base_url)
    unmatched = transclude.unmatched_download_urls(
        reverse_input, named_uploads, base_url
    )

    relink_input = (
        f"[other]({base_url}/experiments.php?mode=view&id=99)\n"
        f'<a href="{base_url}/experiments.php?mode=view&id=99">x</a>\n'
        f"[pasted]({base_url}/experiments.php?mode=view&id=100)\n"
    )
    doc_links = {
        f"{base_url}/experiments.php?mode=view&id=99": {
            "markdown": "../other/report.md",
            "html": "../other/report.md",
        }
    }
    relink_output = transclude.relink(relink_input, doc_links)

    abs_input = "[e](experiments.php?mode=view&id=12) [d](database.php?mode=view&id=5) [a](a.png)\n"
    abs_output = transclude.absolute_links(abs_input, base_url)

    safe_name_inputs = [
        "normal.png",
        "dir/sub/file.csv",
        "dir\\win\\file.csv",
        "~~~tilde.txt",
        "  spaced.txt  ",
        "",
        ".",
        "..",
        "#anchor.csv",
        "a<b>c:d?e`f.txt",
        'a"b|c*d.txt',
        "trailing. ",
        "CON.txt",
        "nul",
        "COM1.csv",
        "LPT9.dat",
        "日本語_図.png",
    ]
    orig_win = transclude._WINDOWS
    try:
        transclude._WINDOWS = False
        safe_posix = {s: transclude.safe_name(s) for s in safe_name_inputs}
        transclude._WINDOWS = True
        safe_windows = {s: transclude.safe_name(s) for s in safe_name_inputs}
    finally:
        transclude._WINDOWS = orig_win

    readable_inputs = [
        ("basic_html", "<h1>Title</h1><p>Hello <b>world</b> &amp; friends.</p>"),
        (
            "figure_and_list",
            (
                '<figure><img src="a.png" width="80%"><figcaption>Fig 1</figcaption></figure>'
                "<ul><li>One</li><li>Two</li></ul>"
            ),
        ),
        (
            "links_and_code",
            '<p>See <a href="https://example.com">link</a> and <code>x &lt; y</code>.</p>',
        ),
        (
            "table",
            "<table><tr><th>A</th><th>B</th></tr><tr><td>1</td><td>2</td></tr></table>",
        ),
    ]
    readable_cases = [
        {
            "name": name,
            "input": html_text,
            "output": transclude.readable_html(html_text),
        }
        for name, html_text in readable_inputs
    ]

    return {
        "extract": extract_cases,
        "has_raw_html": raw_html_cases,
        "bare_paths": bare_path_cases,
        "ignore": ignore_cases,
        "name_uploads": {"input": uploads, "output": named_uploads},
        "download_url": download_url_cases,
        "reverse": {
            "base_url": base_url,
            "uploads": named_uploads,
            "input": reverse_input,
            "output": rev_text,
            "placed_ids": [u["id"] for u in placed],
            "unmatched": unmatched,
        },
        "relink": {
            "input": relink_input,
            "doc_links": doc_links,
            "output": relink_output,
        },
        "absolute_links": {
            "base_url": base_url,
            "input": abs_input,
            "output": abs_output,
        },
        "safe_name": {
            "posix": safe_posix,
            "windows": safe_windows,
        },
        "readable_html": readable_cases,
    }


def main() -> None:
    OUT_DIR.mkdir(parents=True, exist_ok=True)
    for filename, data in (
        ("frontmatter.json", _export_frontmatter()),
        ("transclude.json", _export_transclude()),
    ):
        target = OUT_DIR / filename
        target.write_text(
            json.dumps(data, ensure_ascii=False, indent=2) + "\n",
            encoding="utf-8",
        )
        print(f"wrote {target}")


if __name__ == "__main__":
    main()
