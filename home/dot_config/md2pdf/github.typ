#let horizontalrule = block(above: 1.6em, below: 1.6em, width: 100%,
  line(length: 100%, stroke: 0.6pt + rgb("#d1d9e0")))

#show terms.item: it => block(breakable: false)[
  #text(weight: "bold")[#it.term]
  #block(inset: (left: 1.5em, top: -0.4em))[#it.description]
]

#set table(
  inset: 6pt,
  stroke: none
)

#show figure.where(
  kind: table
): set figure.caption(position: $if(table-caption-position)$$table-caption-position$$else$top$endif$)

#show figure.where(
  kind: image
): set figure.caption(position: $if(figure-caption-position)$$figure-caption-position$$else$bottom$endif$)

$if(highlighting-definitions)$
// syntax highlighting functions from skylighting:
$highlighting-definitions$

$endif$
// GitHub / VS Code markdown-preview styling for pandoc's typst writer.
//
// Why these numbers: they are the GitHub light palette and the proportions the
// VS Code preview uses, converted from CSS px to points (14px body -> 10.5pt).
// Everything that usually makes generated PDFs look wrong is handled here:
// headings get GitHub's underline rules, code blocks get a filled rounded
// panel instead of a bare indent, the horizontal rule spans the text block
// instead of pandoc's default centred 50% stub, and links are GitHub blue
// without an underline.

#let fg       = rgb("#1f2328")   // GitHub body text
#let muted    = rgb("#59636e")   // secondary text, h6, quotes
#let border   = rgb("#d1d9e0")   // rules, table lines
#let codebg   = rgb("#f6f8fa")   // code panels, table header
#let linkcol  = rgb("#0969da")   // GitHub blue

#let conf(
  title: none, subtitle: none, authors: (), keywords: (), date: none,
  lang: "en", region: "US", abstract: none, abstract-title: none, thanks: none,
  margin: (x: 2.2cm, y: 2.2cm),
  paper: "a4",
  // A fallback chain, not a single font. typst resolves missing glyphs by
  // walking this list, and without it anything Helvetica Neue lacks comes out
  // as a tofu box: task-list checkboxes (U+2610 / U+2612) are the common case,
  // and they are exactly what a markdown document uses them for.
  font: ("Helvetica Neue", "Apple Symbols", "Arial Unicode MS", "Apple Color Emoji"),
  fontsize: 10.5pt,
  mathfont: none,
  codefont: ("JetBrainsMono NF",),
  linestretch: 1.0,
  sectionnumbering: none,
  pagenumbering: "1",
  linkcolor: none, citecolor: none, filecolor: none,
  cols: 1,
  doc,
) = {
  set page(
    paper: paper,
    margin: margin,
    numbering: pagenumbering,
    number-align: center,
    footer: context {
      if pagenumbering != none {
        set text(size: 8pt, fill: muted)
        align(center, counter(page).display(pagenumbering))
      }
    },
  )

  // 1.6 line-height, which is what makes the preview readable rather than cramped.
  set text(font: font, size: fontsize, fill: fg, lang: lang, region: region)
  set par(leading: 0.72em * linestretch, justify: false, spacing: 1.15em)
  set heading(numbering: sectionnumbering)

  show link: it => text(fill: linkcol, it)

  // Headings. h1 and h2 carry a hairline rule underneath, which is the single
  // most recognisable thing about the GitHub look.
  show heading: it => {
    let sizes = (20pt, 15.5pt, 13pt, 11.5pt, 10.5pt, 9.5pt)
    let lvl = calc.min(it.level, 6)
    block(
      above: 1.5em,
      below: 0.7em,
      width: 100%,
      // The rule is the block's own bottom border, not a separate line below
      // it: drawn as a sibling paragraph it inherited paragraph spacing and sat
      // a full line adrift of the heading.
      inset: if lvl <= 2 { (bottom: 0.3em) } else { (bottom: 0pt) },
      stroke: if lvl <= 2 { (bottom: 0.6pt + border) } else { none },
    )[
      #set text(size: sizes.at(lvl - 1), weight: "bold",
                fill: if lvl == 6 { muted } else { fg })
      #it.body
    ]
  }

  // Code. A filled rounded panel, breakable so long listings flow across pages.
  show raw.where(block: true): it => block(
    width: 100%,
    fill: codebg,
    radius: 5pt,
    inset: (x: 10pt, y: 9pt),
    breakable: true,
    stroke: 0.5pt + border,
  )[#set text(font: codefont, size: 0.86em); #it]

  show raw.where(block: false): it => box(
    fill: codebg,
    radius: 3pt,
    outset: (y: 0.22em),
    inset: (x: 0.22em),
  )[#set text(font: codefont, size: 0.9em); #it]

  // Blockquote: left rule plus muted text, as in the preview.
  show quote.where(block: true): it => block(
    inset: (left: 1em, top: 0.45em, bottom: 0.45em),
    stroke: (left: 3pt + border),
    width: 100%,
    spacing: 1.2em,
  )[#set text(fill: muted); #it.body]

  // Tables: shaded header, hairline grid.
  set table(
    inset: 7pt,
    stroke: 0.5pt + border,
    fill: (_, y) => if y == 0 { codebg },
  )
  show table.cell.where(y: 0): set text(weight: "bold")
  // pandoc wraps every table in `align(center)` and passes `align: (auto, ...)`,
  // so cells inherit centring and body text comes out ragged-centred. A set-rule
  // scoped to the table resets the context the `auto` columns resolve against,
  // while columns the markdown aligned explicitly (`---:`) still win.
  show table: set align(left)
  show figure.where(kind: table): set align(left)
  show figure.where(kind: image): set align(left)

  set list(indent: 0.35em, spacing: 0.5em, marker: ([•], [◦], [▪]))
  set enum(indent: 0.35em, spacing: 0.5em)
  set terms(indent: 0.6em)

  if title != none {
    block(above: 0pt, below: 1.2em)[
      #set text(size: 22pt, weight: "bold")
      #title
      #if subtitle != none [ #linebreak() #set text(size: 13pt, weight: "regular", fill: muted); #subtitle ]
    ]
  }
  if authors != () or date != none {
    block(below: 1.4em)[
      #set text(size: 9.5pt, fill: muted)
      #authors.map(a => a.name).join(", ")
      #if authors != () and date != none [ · ]
      #date
    ]
  }
  if abstract != none {
    block(inset: (left: 1em), below: 1.4em)[
      #set text(fill: muted)
      #if abstract-title != none [*#abstract-title* #linebreak()]
      #abstract
    ]
  }

  if cols == 1 { doc } else { columns(cols, doc) }
}


$if(smart)$
$else$
#set smartquote(enabled: false)

$endif$
$for(header-includes)$
$header-includes$

$endfor$
#show: doc => conf(
$if(title)$
  title: [$title$],
$endif$
$if(subtitle)$
  subtitle: [$subtitle$],
$endif$
$if(author)$
  authors: (
$for(author)$
$if(author.name)$
    ( name: [$author.name$],
      affiliation: [$author.affiliation$],
      email: [$author.email$] ),
$else$
    ( name: [$author$],
      affiliation: "",
      email: "" ),
$endif$
$endfor$
    ),
$endif$
$if(keywords)$
  keywords: ($for(keywords)$$keywords$$sep$,$endfor$),
$endif$
$if(date)$
  date: [$date$],
$endif$
$if(lang)$
  lang: "$lang$",
$endif$
$if(region)$
  region: "$region$",
$endif$
$if(abstract-title)$
  abstract-title: [$abstract-title$],
$endif$
$if(abstract)$
  abstract: [$abstract$],
$endif$
$if(thanks)$
  thanks: [$thanks$],
$endif$
$if(margin)$
  margin: ($for(margin/pairs)$$margin.key$: $margin.value$,$endfor$),
$endif$
$if(papersize)$
  paper: "$papersize$",
$endif$
$if(mainfont)$
  font: ("$mainfont$",),
$endif$
$if(fontsize)$
  fontsize: $fontsize$,
$endif$
$if(mathfont)$
  mathfont: ($for(mathfont)$"$mathfont$",$endfor$),
$endif$
$if(codefont)$
  codefont: ($for(codefont)$"$codefont$",$endfor$),
$endif$
$if(linestretch)$
  linestretch: $linestretch$,
$endif$
$if(section-numbering)$
  sectionnumbering: "$section-numbering$",
$endif$
  pagenumbering: $if(page-numbering)$"$page-numbering$"$else$none$endif$,
$if(linkcolor)$
  linkcolor: [$linkcolor$],
$endif$
$if(citecolor)$
  citecolor: [$citecolor$],
$endif$
$if(filecolor)$
  filecolor: [$filecolor$],
$endif$
  cols: $if(columns)$$columns$$else$1$endif$,
  doc,
)

$for(include-before)$
$include-before$

$endfor$
$if(toc)$
#outline(
  title: auto,
  depth: $toc-depth$
);
$endif$

$body$

$if(citations)$
$for(nocite-ids)$
#cite(label("${it}"), form: none)
$endfor$
$if(csl)$

#set bibliography(style: "$csl$")
$elseif(bibliographystyle)$

#set bibliography(style: "$bibliographystyle$")
$endif$
$if(bibliography)$

#bibliography(($for(bibliography)$"$bibliography$"$sep$,$endfor$)$if(full-bibliography)$, full: true$endif$)
$endif$
$endif$
$for(include-after)$

$include-after$
$endfor$
