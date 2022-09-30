import { useMemo } from "react";
import heroImage from "../../../assets/logos/kairos-black.svg";
import "./LeftSidebar.css";

const folderOrder = [
  "quickstart",
  "installation",
  "upgrade",
  "architecture",
  "examples",
  "reference",
];

export default function Sidebar({ content, currentPage }) {
  const currentPageMatch = useMemo(
    () => "/" + currentPage.slice(1),
    [currentPage]
  );

  const menu = useMemo(() => {
    const navGroups = content.reduce((acc, { frontmatter, url }) => {
      const partitions = url.split("/");
      if (partitions[1] === partitions[2]) {
        return {
          ...acc,
          [partitions[1]]: {
            url,
            title: frontmatter.title,
            children: [],
          },
        };
      }
      return acc;
    }, {});

    return content.reduce((acc, { frontmatter, url }) => {
      const partitions = url.split("/");
      if (partitions.length <= 1) {
        return acc;
      }
      const folder = partitions[1];
      if (partitions[1] === partitions[2]) {
        return acc;
      }

      return {
        ...acc,
        [folder]: {
          ...acc[folder],
          children: [
            ...(acc?.[folder]?.children || []),
            {
              url,
              title: frontmatter.title,
              index: frontmatter.index,
            },
          ],
        },
      };
    }, navGroups);
  }, [content]);

  return (
     <aside id="grid-left" title="Site Navigation">
      <nav aria-labelledby="grid-left" className="nav">
        <div className="hero-logo">
          <a href="/">
            <img src={heroImage} alt="main logo" width="135" />
          </a>
        </div>
        <ul className="nav-list">
          {folderOrder.map((header, index) => {
            const item = menu[header];

            return (
              <li className="nav-group nav-link" key={index}>
                <strong
                  {...(currentPageMatch === item?.url && {
                    "aria-current": "page",
                  })}
                >
                  {item.title}
                </strong>
                <ul className="nav-list">
                  {(item.children || [])
                    .sort((a, b) => a.index - b.index)
                    .map((child, index) => (
                      <li className="nav-link" key={index}>
                        <a
                          href={child.url}
                          {...(currentPageMatch === child.url && {
                            "aria-current": "page",
                          })}
                        >
                          {child.title}
                        </a>
                      </li>
                    ))}
                </ul>
              </li>
            );
          })}
        </ul>
      </nav>
    </aside>
  );
}
