function urls2epub -d "Convert a list of URLs into an epub via monolith + html-img-optimize + go-readability + pandoc"
    if test (count $argv) -ne 2
        echo "Usage: urls2epub urls.txt output.epub"
        return 1
    end

    set -l urlfile $argv[1]
    set -l output $argv[2]
    set -l tmpdir (mktemp -d /tmp/urls2epub-XXXXXX)
    set -l scriptdir (status dirname)
    set -l idx 0

    if not test -f $urlfile
        echo "Error: $urlfile not found"
        return 1
    end

    for cmd in monolith html-img-optimize go-readability pandoc python3
        if not command -q $cmd
            echo "Error: $cmd not found"
            return 1
        end
    end

    if not test -f $scriptdir/html-normalize-headings.py
        echo "Error: html-normalize-headings.py not found next to this script"
        return 1
    end

    # Phase 1: fetch each URL, optimize images, extract article, normalize headings
    set -l htmlfiles
    for url in (grep -v '^\s*#' $urlfile | grep -v '^\s*$' | string trim)
        set idx (math $idx + 1)
        set -l prefix (printf "%03d" $idx)
        set -l rawfile $tmpdir/$prefix.raw.html
        set -l optfile $tmpdir/$prefix.opt.html
        set -l artfile $tmpdir/$prefix.art.html
        set -l htmlfile $tmpdir/$prefix.html

        echo "[$idx] fetch: $url"
        if not monolith $url -j -F -f -o $rawfile 2>/dev/null
            echo "     ⚠ monolith failed, skipping"
            continue
        end

        set -l raw_bytes (wc -c < $rawfile | string trim)
        echo "     "(math $raw_bytes / 1048576)" MB raw"

        # Optimize images: resize to 800px wide, grayscale, quality 60
        echo "     optimizing images..."
        if not html-img-optimize --max-width 800 --quality 60 --grayscale -o $optfile $rawfile
            echo "     ⚠ html-img-optimize failed, using raw html"
            set optfile $rawfile
        end

        # Remove raw HTML now that optimized version exists
        if test "$optfile" != "$rawfile"
            rm -f $rawfile
        end

        set -l opt_bytes (wc -c < $optfile | string trim)

        # Guard: skip files that are still too large
        if test $opt_bytes -gt 31457280
            echo "     ⚠ still "(math $opt_bytes / 1048576)" MB after optimization, too large — skipping"
            rm -f $optfile
            continue
        end

        # Extract article content (strips nav, footer, ads; keeps images)
        echo "     extracting article..."
        set -l art_title (go-readability -m $optfile 2>/dev/null | python3 -c "import sys,json; print(json.load(sys.stdin).get('title',''))" 2>/dev/null)
        if not go-readability -f $optfile > $artfile 2>/dev/null
            echo "     ⚠ readability extraction failed, using optimized html"
            set artfile $optfile
        end

        rm -f $optfile

        set -l art_bytes (wc -c < $artfile | string trim)
        echo "     "(math $art_bytes / 1024)" KB article"

        # Normalize headings: extract title as H1, shift others down
        set -l title_flag
        if test -n "$art_title"
            set title_flag --title "$art_title"
            echo "     title: $art_title"
        end
        if not python3 $scriptdir/html-normalize-headings.py $artfile -o $htmlfile $title_flag
            echo "     ⚠ heading normalization failed, using as-is"
            set htmlfile $artfile
        else
            rm -f $artfile
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
