import { Route, Routes } from 'react-router'
import Shell from './components/Shell'
import Home from './pages/Home'
import NewWorkshop from './pages/NewWorkshop'
import NotFound from './pages/NotFound'
import Owner from './pages/Owner'
import Participant from './pages/Participant'
import Returned from './pages/Returned'
import Share from './pages/Share'

export default function App() {
  return (
    <Routes>
      <Route element={<Shell />}>
        <Route index element={<Home />} />
        <Route path="new" element={<NewWorkshop />} />
        <Route path="w/:share" element={<Share />} />
        <Route path="p/:token" element={<Participant />} />
        <Route path="owner/:token" element={<Owner />} />
        <Route path="paypal/returned" element={<Returned />} />
        <Route path="*" element={<NotFound />} />
      </Route>
    </Routes>
  )
}
