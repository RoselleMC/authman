import { IconButton } from "../../components/IconButton";
import { useI18n } from "../../i18n/I18nProvider";

interface Props {
  page: number;
  pageSize: number;
  total: number;
  pageSizeOptions: ReadonlyArray<number>;
  onPage: (page: number) => void;
  onPageSize: (size: number) => void;
  testId?: string;
}

/**
 * Bottom-bar pagination: page size on the left, "showing X–Y of Z" centered,
 * prev/next on the right. Keyboard accessible via the underlying IconButton
 * controls and the labelled <select>.
 */
export function Pagination({
  page,
  pageSize,
  total,
  pageSizeOptions,
  onPage,
  onPageSize,
  testId,
}: Props) {
  const { t } = useI18n();
  const totalPages = Math.max(1, Math.ceil(total / Math.max(1, pageSize)));
  const clampedPage = Math.min(Math.max(1, page), totalPages);
  const start = total === 0 ? 0 : (clampedPage - 1) * pageSize + 1;
  const end = total === 0 ? 0 : Math.min(total, clampedPage * pageSize);

  return (
    <div className="adv-list-pagination" data-testid={testId}>
      <div className="adv-list-pagination__size">
        <label className="adv-list-pagination__size-label" htmlFor={`${testId ?? "list"}-page-size`}>
          {t("list.rowsPerPage")}
        </label>
        <div className="select-box">
          <select
            id={`${testId ?? "list"}-page-size`}
            className="select"
            value={pageSize}
            onChange={(e) => onPageSize(Number(e.target.value))}
            data-testid={testId ? `${testId}-page-size` : "list-page-size"}
            aria-label={t("list.rowsPerPage")}
          >
            {pageSizeOptions.map((opt) => (
              <option key={opt} value={opt}>
                {opt}
              </option>
            ))}
          </select>
        </div>
      </div>
      <div
        className="adv-list-pagination__info"
        aria-live="polite"
        data-testid={testId ? `${testId}-page-info` : "list-page-info"}
      >
        {total === 0 ? `0 ${t("list.of")} 0` : `${start.toLocaleString()}–${end.toLocaleString()} ${t("list.of")} ${total.toLocaleString()}`}
      </div>
      <div className="adv-list-pagination__nav">
        <IconButton
          bordered
          name="chevronLeft"
          size={15}
          label={t("list.previousPage")}
          disabled={clampedPage <= 1}
          onClick={() => onPage(clampedPage - 1)}
          data-testid={testId ? `${testId}-page-prev` : "list-page-prev"}
        />
        <span className="adv-list-pagination__pageno" data-testid={testId ? `${testId}-page-no` : "list-page-no"}>
          {t("list.page")} {clampedPage} / {totalPages}
        </span>
        <IconButton
          bordered
          name="chevronRight"
          size={15}
          label={t("list.nextPage")}
          disabled={clampedPage >= totalPages}
          onClick={() => onPage(clampedPage + 1)}
          data-testid={testId ? `${testId}-page-next` : "list-page-next"}
        />
      </div>
    </div>
  );
}
