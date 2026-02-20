#!/usr/bin/env python3
"""Normalize HTML headings for epub chapter structure.

Extracts the article title, demotes all content headings to H2+,
and prepends a single H1 with the title. This ensures each file
becomes one chapter when pandoc builds the epub with --split-level=1.

Usage: html-normalize-headings.py input.html > output.html
       html-normalize-headings.py input.html -o output.html
"""
import re
import sys
import html


def extract_title(text):
    """Extract article title from <title> tag or first <h1>."""
    # Try <title> first
    m = re.search(r'<title>([^<]+)</title>', text, re.IGNORECASE)
    if m:
        title = clean_title(html.unescape(m.group(1)).strip())
        if title and title != "Untitled":
            return title

    # Fall back to first <h1>
    m = re.search(r'<h1[^>]*>(.*?)</h1>', text, re.DOTALL | re.IGNORECASE)
    if m:
        return re.sub(r'<[^>]+>', '', m.group(1)).strip()

    return "Untitled"


def shift_headings(text):
    """Shift all headings down by one level (h1->h2, h2->h3, etc).
    h5 and h6 become h6 (clamped)."""
    def replace_heading(m):
        tag = m.group(1)  # 'h1' or '/h1' etc
        is_close = tag.startswith('/')
        if is_close:
            level = int(tag[2])
        else:
            level = int(tag[1])
        new_level = min(level + 1, 6)
        if is_close:
            return f'</h{new_level}>'
        else:
            attrs = m.group(2)
            return f'<h{new_level}{attrs}>'

    return re.sub(
        r'<(/?)h([1-6])([^>]*)>',
        lambda m: f'<{m.group(1)}h{min(int(m.group(2))+1, 6)}{m.group(3) if not m.group(1) else ""}>',
        text, flags=re.IGNORECASE
    )


def clean_title(title):
    """Remove common site name suffixes."""
    title = re.split(r'\s*[-|–—]\s+', title)[0].strip()
    return title or "Untitled"


def normalize(text, title_override=None):
    title = clean_title(title_override) if title_override else extract_title(text)

    # Shift all existing headings down one level
    text = shift_headings(text)

    # Insert H1 title right after <body> (or at start if no body tag)
    title_html = f'<h1>{html.escape(title)}</h1>\n'
    m = re.search(r'(<body[^>]*>)', text, re.IGNORECASE)
    if m:
        pos = m.end()
        text = text[:pos] + '\n' + title_html + text[pos:]
    else:
        text = title_html + text

    return text


def main():
    import argparse
    p = argparse.ArgumentParser(description=__doc__,
                                formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument('input', help='Input HTML file')
    p.add_argument('-o', '--output', help='Output file (default: stdout)')
    p.add_argument('--title', help='Override article title (used when title is not in the HTML)')
    args = p.parse_args()

    with open(args.input, 'r', errors='replace') as f:
        text = f.read()

    result = normalize(text, title_override=args.title)

    if args.output:
        with open(args.output, 'w') as f:
            f.write(result)
    else:
        sys.stdout.write(result)


if __name__ == '__main__':
    main()
