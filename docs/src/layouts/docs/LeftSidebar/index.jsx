import {  useMemo } from "react";
import heroImage from '../../../assets/logos/kairos-dark-column-docs.png';
import "./LeftSidebar.css";

const folderOrder = ["quickstart", "installation", "upgrade", "architecture", "examples", "reference"]

export default function Sidebar({ content, currentPage }) {
  const currentPageMatch = useMemo(
    () => "/" + currentPage.slice(1),
    [currentPage]
  );

  const menu = useMemo(() => {
    const navGroups = content.reduce((acc, { frontmatter, url }) => {
      const relativePath = url.split("content/")[1];
      const partitions = relativePath.split("/");
      if (partitions[0] === partitions[1]) {
        return {
          ...acc,
          [partitions[0]]: {
            url,
            title: frontmatter.title,
            children: [],
          },
        };
      }
      return acc;
    }, {});

    return content.reduce((acc, { frontmatter, url }) => {
      const relativePath = url.split("content/")[1];
      const partitions = relativePath?.split("/");
      const folder = partitions[0];

      if (partitions[0] === partitions[1]) {
        return acc;
      }

      return {
        ...acc,
        [folder]: {
          ...acc[folder],
          children: [
            ...acc[folder]?.children,
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
    <nav aria-labelledby="grid-left" className="nav">
      <div className="hero-logo">
      <img src={heroImage} alt="main logo" width="135" />
      </div>
      <ul className="nav-list">
        {folderOrder.map((header, index) => {
          const item = menu[header];

          return (
            <li id="nav-group" className="nav-link" key={index}>
              <a
                href={item.url}
                {...(currentPageMatch === item.url && {
                  "aria-current": "page",
                })}
              >
                {item.title}
              </a>
              <ul className="nav-list">
                {item.children
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
  );
}
