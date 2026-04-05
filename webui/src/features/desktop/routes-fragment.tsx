import { Route } from "react-router-dom";
import { DesktopHelpPage } from "./DesktopHelpPage";
import { DesktopStatusPage } from "./DesktopStatusPage";
import { DESKTOP_HELP_ROUTE, DESKTOP_STATUS_ROUTE } from "./session";

export function renderDesktopRoutesFragment() {
  return (
    <>
      <Route path={DESKTOP_HELP_ROUTE} element={<DesktopHelpPage />} />
      <Route path={DESKTOP_STATUS_ROUTE} element={<DesktopStatusPage />} />
    </>
  );
}
