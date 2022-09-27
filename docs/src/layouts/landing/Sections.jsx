import "./Sections.css";
import explore from "../../assets/index/explore.png";

const MAIN_SECTIONS = [
  {
    title: "Key Values",
    direction: "vertical",
    sections: [
      {
        title: "Immutable",
        icon: explore,
        sections: [
          {
            title: "Layout",
            description:
              "An immutable OS is a carefully engineered system which boots in a restricted, permissionless mode, where certain paths of the system are not writeable.",
          },
          {
            title: "Container based OS",
            description:
              "An immutable OS is a carefully engineered system which boots in a restricted, permissionless mode, where certain paths of the system are not writeable.",
          },
          {
            title: "Bring your own OS",
            description:
              "An immutable OS is a carefully engineered system which boots in a restricted, permissionless mode, where certain paths of the system are not writeable.",
          },
        ],
      },
      {
        title: "Flexibility",
        icon: explore,
        sections: [
          {
            title: "Ease of use",
            description:
              "An immutable OS is a carefully engineered system which boots in a restricted, permissionless mode, where certain paths of the system are not writeable.",
          },
          {
            title: "Plugs into existing pipelines",
            description:
              "An immutable OS is a carefully engineered system which boots in a restricted, permissionless mode, where certain paths of the system are not writeable.",
          },
          {
            title: "K8s management",
            description:
              "An immutable OS is a carefully engineered system which boots in a restricted, permissionless mode, where certain paths of the system are not writeable.",
          },
        ],
      },
    ],
  },
  {
    title: "Learn about Kairos",
    direction: "horizontal",
    sections: [
      {
        title: "Layout",
        icon: explore,
        description:
          "An immutable OS is a carefully engineered system which boots in a restricted, permissionless mode, where certain paths of the system are not writeable.",
      },
      {
        title: "Container based OS",
        icon: explore,
        description:
          "An immutable OS is a carefully engineered system which boots in a restricted, permissionless mode, where certain paths of the system are not writeable.",
      },
      {
        title: "Bring your own OS",
        icon: explore,
        description:
          "An immutable OS is a carefully engineered system which boots in a restricted, permissionless mode, where certain paths of the system are not writeable.",
      },
    ],
  },
  {
    title: "Links",
    direction: "horizontal",
    sections: [
      {
        title: "Contribute on Github",
        icon: explore,
      },
      {
        title: "Documentation",
        icon: explore,
      },
    ],
  },
];

export default function Sections() {
  function renderTertiarySections(section, index) {
    return (
      <div className="tertiary-section" key={index}>
        <div className="title">{section.title}</div>
        <p>{section.description}</p>
      </div>
    );
  }
  function renderSecondarySections(section, index) {
    return (
      <div className="secondary-section" key={index}>
        <div className="title">
          <img src={section.icon} alt="section logo" width="112" />
          <div>{section.title}</div>
          <p>{section.description}</p>
        </div>
        {section.sections?.length && (
          <div>{section.sections.map(renderTertiarySections)}</div>
        )}
      </div>
    );
  }

  function renderMainSections(section, index) {
    return (
      <div className="main-section" key={index}>
        <div className="title">{section.title}</div>
        <div className="sections" aria-current={section.direction}>
          {section.sections.map(renderSecondarySections)}
        </div>
      </div>
    );
  }
  return (
    <div className="sections">{MAIN_SECTIONS.map(renderMainSections)}</div>
  );
}
