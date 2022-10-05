import "./RightSidebar.css";

export default function RightSidebar({ headings }) {
  const secondDepthHeadings = headings.filter((item) => item.depth === 2);
  if (!secondDepthHeadings.length) {
    return null;
  }

  return (
    <aside id="grid-right" title="Table of Contents">
      <nav className="toc">
        <h4>Table of contents</h4>
        <ul>
          {secondDepthHeadings.map((heading) => (
            <li className="toc-item">
              <a href={`#${heading.slug}`}>{heading.text}</a>
            </li>
          ))}
        </ul>
      </nav>
    </aside>
  );
}
