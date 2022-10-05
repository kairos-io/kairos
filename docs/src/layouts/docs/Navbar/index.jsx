import { useState } from "react";
import LeftSideBar from "../LeftSidebar/index";
import { FontAwesomeIcon } from "@fortawesome/react-fontawesome";
import { faBars } from "@fortawesome/free-solid-svg-icons";
import "./Navbar.css";

export default function Navbar(props) {
  const [hidden, setNavbarVisibility] = useState(true);

  return (
    <>
      <div className="navbar-wrap">
        <FontAwesomeIcon
          icon={faBars}
          id="hamburger"
          onClick={() => {
            setNavbarVisibility(!hidden);
          }}
        />
      </div>
      <div className={`${hidden ? "sidebar-hidden" : "sidebar"}`}>
        <LeftSideBar
          {...props}
          cb={() => {
            setNavbarVisibility(true);
          }}
        />
        <div
          className={`${hidden ? "overlay-hidden" : "overlay"}`}
          onClick={() => {
            setNavbarVisibility(!hidden);
          }}
        />
      </div>
    </>
  );
}
