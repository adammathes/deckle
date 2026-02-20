function urls2epub -d "Convert a list of URLs into an epub via url2html + pandoc"
    if test (count $argv) -ne 2
        echo "Usage: urls2epub urls.txt output.epub"
        return 1
    end

    set -l urlfile $argv[1]
    set -l output $argv[2]
    set -l tmpdir (mktemp -d /tmp/urls2epub-XXXXXX)
    set -l idx 0

    if not test -f $urlfile
        echo "Error: $urlfile not found"
        return 1
    end

    for cmd in url2html pandoc
        if not command -q $cmd
            echo "Error: $cmd not found on PATH"
            return 1
        end
    end

    # Phase 1: fetch each URL and produce clean HTML
    set -l htmlfiles
    for url in (grep -v '^\s*#' $urlfile | grep -v '^\s*$' | string trim)
        set idx (math $idx + 1)
        set -l prefix (printf "%03d" $idx)
        set -l htmlfile $tmpdir/$prefix.html

        echo "[$idx] $url"
        if not url2html --grayscale -o $htmlfile $url 2>&1 | while read line
                echo "     $line"
            end
            echo "     ⚠ failed, skipping"
            rm -f $htmlfile
            continue
        end

        if not test -f $htmlfile
            echo "     ⚠ no output, skipping"
            continue
        end

        set -l html_bytes (wc -c < $htmlfile | string trim)

        # Guard: skip files that are still too large for pandoc
        if test $html_bytes -gt 31457280
            echo "     ⚠ "(math $html_bytes / 1048576)" MB — too large, skipping"
            rm -f $htmlfile
            continue
        end

        set -a htmlfiles $htmlfile
    end

    if test (count $htmlfiles) -eq 0
        echo "Error: no articles converted"
        rm -rf $tmpdir
        return 1
    end

    # Phase 2: combine all HTML into one epub
    set -l title (string replace -r '\.epub$' '' (basename $output))

    echo "Building epub from" (count $htmlfiles) "articles..."
    pandoc $htmlfiles \
        -f html \
        -t epub3 \
        -o $output \
        --metadata title="$title" \
        --toc \
        --toc-depth=2 \
        --split-level=1

    if test $status -eq 0
        echo "✓ $output ("(count $htmlfiles)" articles)"
    else
        echo "Error: pandoc epub build failed"
        echo "Temp files preserved: $tmpdir"
        return 1
    end

    rm -rf $tmpdir
end
