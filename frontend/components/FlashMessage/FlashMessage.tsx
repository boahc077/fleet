import React, { useEffect, useState } from "react";
import classnames from "classnames";

import { INotification } from "interfaces/notification";
// @ts-ignore
import FleetIcon from "components/icons/FleetIcon";
import Button from "components/buttons/Button";

import CloseIcon from "../../../assets/images/icon-close-white-16x16@2x.png";
import CloseIconBlack from "../../../assets/images/icon-close-fleet-black-16x16@2x.png";
import ErrorIcon from "../../../assets/images/icon-error-white-16x16@2x.png";

const baseClass = "flash-message";

export interface IFlashMessage {
  fullWidth: boolean;
  notification: INotification | null;
  isPersistent?: boolean;
  className?: string;
  onRemoveFlash: () => void;
  onUndoActionClick?: (
    value: () => void
  ) => (evt: React.MouseEvent<HTMLButtonElement, MouseEvent>) => void;
}

const FlashMessage = ({
  fullWidth,
  notification,
  isPersistent,
  className,
  onRemoveFlash,
  onUndoActionClick,
}: IFlashMessage): JSX.Element | null => {
  const { alertType, isVisible, message, undoAction } = notification || {};
  const baseClasses = classnames(
    baseClass,
    className,
    `${baseClass}--${alertType}`,
    {
      [`${baseClass}--full-width`]: fullWidth,
    }
  );

  const [hide, setHide] = useState(false);

  // This useEffect handles hiding successful flash messages after a 4s timeout. By putting the
  // notification in the dependency array, we can properly reset whenever a new flash message comes through.
  useEffect(() => {
    // Any time this hook runs, we reset the hide to false (so that subsequent messages that will be
    // using this same component instance will be visible).
    setHide(false);

    if (!isPersistent && alertType === "success" && isVisible) {
      // After 4 seconds, set hide to true.
      const timer = setTimeout(() => {
        setHide(true);
        onRemoveFlash(); // This function resets notifications which allows CoreLayout reset of selected rows
      }, 4000);
      // Return a cleanup function that will clear this reset, in case another render happens
      // after this. We want that render to set a new timeout (if needed).
      return () => clearTimeout(timer);
    }

    return undefined; // No cleanup when we don't set a timeout.
  }, [notification, alertType, isVisible, setHide]);

  if (hide || !isVisible) {
    return null;
  }

  return (
    <div className={baseClasses} id={baseClasses}>
      <div className={`${baseClass}__content`}>
        {alertType === "success" ? (
          <FleetIcon name="success-check" />
        ) : (
          <img alt="error icon" src={ErrorIcon} />
        )}
        <span>{message}</span>
        {onUndoActionClick && undoAction && (
          <Button
            className={`${baseClass}__undo`}
            variant="unstyled"
            onClick={onUndoActionClick(undoAction)}
          >
            Undo
          </Button>
        )}
      </div>
      <div className={`${baseClass}__action`}>
        <div className={`${baseClass}__ex`}>
          <button
            className={`${baseClass}__remove ${baseClass}__remove--${alertType} button--unstyled`}
            onClick={onRemoveFlash}
          >
            <img
              src={alertType === "warning-filled" ? CloseIconBlack : CloseIcon}
              alt="close icon"
            />
          </button>
        </div>
      </div>
    </div>
  );
};

export default FlashMessage;
