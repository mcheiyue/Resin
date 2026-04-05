import { Route } from "react-router-dom";
import { DesktopHelpPage } from "./DesktopHelpPage";
import { DesktopStatusPage } from "./DesktopStatusPage";
import { getDesktopDefaultEntryPath, getDesktopHelpRoute } from "./session";

export function DesktopRoutesFragment() {
  return (
    <>
      <Route path={getDesktopHelpRoute()} element={<DesktopHelpPage />} />
      <Route path={getDesktopDefaultEntryPath()} element={<DesktopStatusPage />} />
    </>
  );
}
