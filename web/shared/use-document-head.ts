import { useEffect } from "react";

export function useDocumentHead(opts: { title?: string; description?: string }) {
  useEffect(() => {
    if (opts.title) document.title = opts.title;
    if (opts.description) {
      const meta = document.querySelector('meta[name="description"]');
      if (meta) meta.setAttribute("content", opts.description);
    }
  }, [opts.title, opts.description]);
}
