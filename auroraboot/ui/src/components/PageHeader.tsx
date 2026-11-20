import type { ReactNode } from "react";
import { Link } from "react-router";
import { ChevronRight } from "lucide-react";

export interface BreadcrumbItem {
  label: string;
  to?: string;
}

interface PageHeaderProps {
  title: string;
  description?: string;
  breadcrumb?: BreadcrumbItem[];
  // A status badge shown next to the title.
  status?: ReactNode;
  // A line of facts under the title, such as an address or a version.
  meta?: ReactNode;
  // The page actions, in placement order: secondary, primary, then the ⋯ menu.
  children?: ReactNode;
}

export function PageHeader({ title, description, breadcrumb, status, meta, children }: PageHeaderProps) {
  return (
    <div className="mb-8">
      {breadcrumb && breadcrumb.length > 0 && (
        <nav aria-label="Breadcrumb" className="mb-2">
          <ol className="flex flex-wrap items-center gap-1 text-sm text-muted-foreground">
            {breadcrumb.map((item, i) => (
              <li key={`${i}-${item.label}`} className="flex min-w-0 items-center gap-1">
                {i > 0 && <ChevronRight className="h-3.5 w-3.5 shrink-0" aria-hidden="true" />}
                {item.to ? (
                  <Link to={item.to} className="truncate hover:text-foreground hover:underline">
                    {item.label}
                  </Link>
                ) : (
                  <span className="truncate" aria-current={i === breadcrumb.length - 1 ? "page" : undefined}>
                    {item.label}
                  </span>
                )}
              </li>
            ))}
          </ol>
        </nav>
      )}
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-3">
            <h1 className="min-w-0 break-words text-3xl font-bold tracking-tight">{title}</h1>
            {status}
          </div>
          {description && <p className="text-muted-foreground mt-1">{description}</p>}
          {meta && (
            <div className="mt-1 flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-muted-foreground">
              {meta}
            </div>
          )}
        </div>
        {children && <div className="flex flex-wrap items-center gap-2">{children}</div>}
      </div>
    </div>
  );
}
